package main

import (
	"context"
	"flag"
	"fmt"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"log"
	"regexp"
	"strconv"
)

func main() {

	mongoUri := flag.String("mongo-uri", "", "Mongo-db uri to download from")
	mongoDb := flag.String("mongo-db", "", "Mongo-db database name")
	limit := flag.Int64("limit", 100, "number of records to read from mongo-db")
	skip := flag.Int64("skip", 0, "number of records to skip from mongo-db")
	influxUri := flag.String("influx-uri", "", "InfluxDb uri to download from")
	influxToken := flag.String("influx-token", "", "InfluxDb uri to download from")

	flag.Parse()

	reg := regexp.MustCompile("Dev: (?P<dev>[-0-9.]+),.*ISF: (?P<isf>[-0-9.]+),.*CR: (?P<cr>[-0-9.]+)")
	ctx := context.Background()

	client, err := mongo.NewClient(options.Client().ApplyURI(*mongoUri))
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err = client.Disconnect(ctx); err != nil {
			panic(err)
		}
	}()

	err = client.Connect(ctx)
	if err != nil {
		log.Fatal(err)
	}

	err = client.Ping(ctx, nil)
	if err != nil {
		log.Fatal(err)
	}

	influx := influxdb2.NewClient(*influxUri, *influxToken)
	writeAPI := influx.WriteAPIBlocking("ns", "ns")

	collection := client.Database(*mongoDb).Collection("devicestatus")
	filter := bson.D{{"openaps", bson.D{{"$exists", true}}}}

	opts := options.Find()
	opts.SetSort(bson.D{{"created_at", -1}})
	opts.SetLimit(*limit)
	if *skip > 0 {
		opts.SetSkip(*skip)
	}

	cur, err := collection.Find(ctx, filter, opts)
	if err != nil {
		log.Fatal(err)
	}
	defer cur.Close(ctx)

	for cur.Next(ctx) {
		var entry NsEntry
		err := cur.Decode(&entry)
		if err != nil {
			log.Fatal(err)
		}

		point := influxdb2.NewPointWithMeasurement("openaps").
			AddField("iob", entry.OpenAps.IOB.IOB).
			AddField("basal_iob", entry.OpenAps.IOB.BasalIOB).
			AddField("activity", entry.OpenAps.IOB.Activity).
			SetTime(entry.OpenAps.IOB.Time)
		if entry.OpenAps.Suggested.Bg > 0 {
			point.
				AddField("bg", entry.OpenAps.Suggested.Bg).
				AddField("eventual_bg", entry.OpenAps.Suggested.EventualBG).
				AddField("target_bg", entry.OpenAps.Suggested.TargetBG).
				AddField("insulin_req", entry.OpenAps.Suggested.InsulinReq).
				AddField("cob", entry.OpenAps.Suggested.COB).
				AddField("bolus", entry.OpenAps.Suggested.Units).
				AddField("tbs_rate", entry.OpenAps.Suggested.Rate).
				AddField("tbs_duration", entry.OpenAps.Suggested.Duration)
			if len(entry.OpenAps.Suggested.PredBGs.COB) > 0 {
				point.AddField("pred_cob", entry.OpenAps.Suggested.PredBGs.COB[len(entry.OpenAps.Suggested.PredBGs.COB)-1])
			}
			if len(entry.OpenAps.Suggested.PredBGs.IOB) > 0 {
				point.AddField("pred_iob", entry.OpenAps.Suggested.PredBGs.IOB[len(entry.OpenAps.Suggested.PredBGs.IOB)-1])
			}
			if len(entry.OpenAps.Suggested.PredBGs.UAM) > 0 {
				point.AddField("pred_uam", entry.OpenAps.Suggested.PredBGs.UAM[len(entry.OpenAps.Suggested.PredBGs.UAM)-1])
			}
			if len(entry.OpenAps.Suggested.PredBGs.ZT) > 0 {
				point.AddField("pred_zt", entry.OpenAps.Suggested.PredBGs.ZT[len(entry.OpenAps.Suggested.PredBGs.ZT)-1])
			}
			if len(entry.OpenAps.Suggested.Reason) > 0 {
				matches := reg.FindStringSubmatch(entry.OpenAps.Suggested.Reason)
				names := reg.SubexpNames()
				for i, match := range matches {
					if i != 0 {
						if len(match) > 0 {
							if rvalue, err := strconv.ParseFloat(match, 32); err == nil {
								point.AddField(names[i], rvalue)
							}
						}
					}
				}
			}
		}

		err = writeAPI.WritePoint(context.Background(), point)
		if err != nil {
			log.Fatal(err)
		}

		fmt.Println("time: ", entry.OpenAps.IOB.Time, ", bg: ", entry.OpenAps.Suggested.Bg)
	}
	if err := cur.Err(); err != nil {
		log.Fatal(err)
	}

}
