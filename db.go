package main

import (
	"fmt"
	"log"
	"time"

	client "github.com/influxdata/influxdb1-client/v2"
)

func connectInfluxdb(config config) (client.Client, error) {
	// Open the influxdb connection
	if config.InfluxdbServer == "" {
		log.Fatal("No Influxdb address set")
	}
	Conn, err := client.NewHTTPClient(client.HTTPConfig{
		Addr: config.InfluxdbServer, Username: config.InfluxdbServer, Password: config.InfluxPassword, Timeout: time.Duration(180 * time.Second)})
	if err != nil {
		log.Print(err)
		return nil, err
	}
	return Conn, nil
}

// FIX this... Really I need to invest time in Fixing this.. But it works mostly.
func dbWorker(config config, dbqueue chan client.BatchPoints) {
	// Open the connection to the InfluxDB
	conn, err := connectInfluxdb(config)
	if err != nil {
		log.Fatal("Unable to connect to DB at Startup")
	}
	defer conn.Close()
	var errcount int

	// Read forever from the dbqueue chan looking for new Batchpoints to save to the DB.
	for bp := range dbqueue {
		// Write the Batch point
		if err := conn.Write(bp); err != nil {
			log.Print(err)
			// Lets check to see if we can still get to the db
			_, _, err := conn.Ping(time.Second * 10)
			if err != nil {
				log.Print(err)
				// Since we still can't get the db lets sleep for awhile before trying again
				errcount++
				errmessage := fmt.Sprintf("Sleeping %d seconds before trying to reconnect\n", 30*errcount)
				log.Print(errmessage)
				conn.Close()
				time.Sleep(time.Duration(30*errcount) * time.Second)
				conn, err = connectInfluxdb(config)
				if err != nil {
					log.Print("Unable to reconnect to DB")
				}
			}
		} else {
			errcount = 0
		}
	}

}

func newInfluxBP(config config) client.BatchPoints {
	// Get a new Batch point
	bp, err := client.NewBatchPoints(client.BatchPointsConfig{
		Database: config.InfluxDB, Precision: "s"})
	if err != nil {
		log.Print(err)
	}
	return bp
}

func queuePointint(measure string, tags map[string]string, intValue int, bp client.BatchPoints) {
	// Create and add a point to the batch point.
	value := float64(intValue)

	fields := map[string]interface{}{
		"value": value}
	pt, err := client.NewPoint(measure, tags, fields, time.Now())
	if err != nil {
		log.Print(err)
	}
	bp.AddPoint(pt)
}

func queuePointUint64(measure string, tags map[string]string, Uint64Value uint64, bp client.BatchPoints) {
	// Create and add a point to the batch point.
	value := float64(Uint64Value)

	fields := map[string]interface{}{
		"value": value}
	pt, err := client.NewPoint(measure, tags, fields, time.Now())
	if err != nil {
		log.Print(err)
	}
	bp.AddPoint(pt)
}

func queuePointFloat64(measure string, tags map[string]string, value float64, bp client.BatchPoints) {
	// Create and add a point to the batch point.
	fields := map[string]interface{}{
		"value": value}
	pt, err := client.NewPoint(measure, tags, fields, time.Now())
	if err != nil {
		log.Print(err)
	}
	bp.AddPoint(pt)
}
