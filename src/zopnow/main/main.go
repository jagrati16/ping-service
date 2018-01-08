package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"log"
	"sync"
	"time"
	"zopnow/utils"
)

var db *sql.DB

//Initialize the database
func init() {
	var err error
	//A DSN in its fullest form
	db, err = NewDB("root:@/organization_service")
	if err != nil {
		panic(err)
	}
	db.SetMaxOpenConns(20)
}

func NewDB(dataSourceName string) (*sql.DB, error) {
	db, err := sql.Open("mysql", dataSourceName)
	if err != nil {
		return nil, err
	}
	if err = db.Ping(); err != nil {
		return nil, err
	}
	return db, nil
}

type organizationData struct {
	id     int
	name   string
	domain string
}

type Tag struct {
	Organization   string `json:"organization"`
	OrganizationId int    `json:"organization_id"`
}

type DataPoint struct {
	Metric    string `json:"metric"`
	Timestamp int64  `json:"timestamp"`
	Value     int64  `json:"value"`
	Tags      Tag    `json:"tags"`
}

// this function will execute http request and return
// if request is servicable and Request ttfb and ttlb values
func traceUrl(domain string) (int64, int64, int64, int64, int64, int64) {
	var err error
	var pageLoadTime = int64(-1)
	servicable, ttfb, ttlb, dns, ssl := utils.SendRequest("http://" + domain + "/favicon.ico")
	if servicable == 1 {
		pageLoadTime, err = utils.CalculatePageLoadTime("http://" + domain)
		if err != nil {
			log.Println(err)
		}
	} else {
		servicable, ttfb, ttlb, dns, ssl = utils.SendRequest("https://" + domain + "/favicon.ico")
		if servicable == 1 {
			pageLoadTime, err = utils.CalculatePageLoadTime("http://" + domain)
			if err != nil {
				log.Println(err)
			}
		}
	}
	log.Println("............", servicable, ttfb, ttlb, dns, ssl, pageLoadTime)
	return servicable, ttfb, ttlb, dns, ssl, pageLoadTime
}

func processRows(rows []organizationData, timestamp int64) {
	var dataPoints []DataPoint

	for i := 0; i < len(rows); i++ {
		var row = rows[i]
		var domain = row.domain
		var uptime DataPoint
		var ttfb DataPoint
		var ttlb DataPoint
		var dns DataPoint
		var ssl DataPoint
		var pageLoad DataPoint

		uptime.Metric = "domain.uptime"
		uptime.Timestamp = timestamp
		uptime.Tags = Tag{domain, row.id}

		ttfb.Metric = "domain.ttfb"
		ttfb.Timestamp = timestamp
		ttfb.Tags = Tag{domain, row.id}

		ttlb.Metric = "domain.ttlb"
		ttlb.Timestamp = timestamp
		ttlb.Tags = Tag{domain, row.id}

		dns.Metric = "domain.dns"
		dns.Timestamp = timestamp
		dns.Tags = Tag{domain, row.id}

		ssl.Metric = "domain.ssl"
		ssl.Timestamp = timestamp
		ssl.Tags = Tag{domain, row.id}

		pageLoad.Metric = "domain.pageLoad"
		pageLoad.Timestamp = timestamp
		pageLoad.Tags = Tag{domain, row.id}

		uptime.Value, ttfb.Value, ttlb.Value, dns.Value, ssl.Value, pageLoad.Value = traceUrl(domain)
		dataPoints = append(dataPoints, uptime, ttfb, ttlb, dns, ssl)
	}

	points, err := json.Marshal(dataPoints)
	if err != nil {
		log.Println(err)
	}
	utils.SendDataToDB(points, len(dataPoints))
}

func sliceRows(orgData []organizationData) [][]organizationData {
	var divided [][]organizationData
	chunkSize := 10
	for i := 0; i < len(orgData); i += chunkSize {
		end := i + chunkSize

		if end > len(orgData) {
			end = len(orgData)
		}
		divided = append(divided, orgData[i:end])
	}
	return divided
}

func main() {
	var orgData []organizationData
	var wg sync.WaitGroup
	var timestamp = time.Now().Unix()

	// query to get all the organization-domain details
	rows, err := db.Query("select id,name,domain from organizations where deleted_at is null and domain is not null")
	if err != nil {
		fmt.Println(err)
	}
	defer rows.Close()

	for rows.Next() {
		var data organizationData
		err := rows.Scan(&data.id, &data.name, &data.domain)
		if err != nil {
			fmt.Println(err)
		}
		orgData = append(orgData, data)
	}

	// slice the data rows in chunks of 10
	chunks := sliceRows(orgData)

	// for each chunk run the `process rows` function in goroutine
	for _, chunk := range chunks {
		wg.Add(1)
		go func(chunk []organizationData, wg *sync.WaitGroup, timestamp int64) {
			defer wg.Done()
			processRows(chunk, timestamp)
		}(chunk, &wg, timestamp)
	}
	wg.Wait()
}
