package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"log"
	"net/http"
	"net/http/httptrace"
	"sync"
	"time"
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

type transport struct {
	current *http.Request
}

// timeout http get request after 10 seconds
var timeout = time.Duration(10 * time.Second)
var client = http.Client{
	Timeout:   timeout,
	Transport: &transport{},
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.current = req
	return http.DefaultTransport.RoundTrip(req)
}

func sendRequest(url string) (int64, int64, int64) {
	var start time.Time
	var ttfb, ttlb time.Duration
	servicable := int64(1)
	req, _ := http.NewRequest("GET", url, nil)
	trace := &httptrace.ClientTrace{
		GotFirstResponseByte: func() {
			ttfb = time.Since(start)
		},
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
	start = time.Now()
	resp, err := client.Do(req)
	ttlb = time.Since(start)
	if err != nil {
		servicable = 0
		log.Println("For URl: "+url+" TTFB: ", ttfb, " TTLB: ", ttlb, " Error: ", err)
	} else {
		if resp.StatusCode != 200 {
			servicable = 0
		}
		log.Println("For URl: "+url+" httpStatusCode:", resp.StatusCode, " TTFB:", ttfb, " TTLB:", ttlb, "\n")
	}
	return servicable, int64(ttfb / time.Microsecond), int64(ttlb / time.Microsecond)
}

// this function will execute http request and return
// if request is servicable and Request ttfb and ttlb values
func traceUrl(domain string) (int64, int64, int64) {
	servicable, ttfb, ttlb := sendRequest("http://" + domain + "/favicon.ico")
	if servicable == 0 {
		servicable, ttfb, ttlb = sendRequest("https://" + domain + "/favicon.ico")
	}
	return servicable, ttfb, ttlb
}

func sendDataToDB(points []byte, numberOfPoints int) {
	req, err := http.NewRequest("POST", "http://localhost:4242/api/put", bytes.NewBuffer(points))
	if err != nil {
		log.Println(err)
	}
	req.Header.Set("Content-Type", "application/json")
	dbClient := &http.Client{}
	resp, err := dbClient.Do(req)
	if err != nil {
		log.Println(err)
		log.Println("Could not send ", numberOfPoints, " Data Points to OpenTSDB")
	} else {
		defer resp.Body.Close()
		log.Println("response Status:", resp.Status)
		log.Println("response Headers:", resp.Header)
		log.Println("Sent ", numberOfPoints, " Data Points to OpenTSDB")
	}

}

func processRows(rows []organizationData, timestamp int64) {
	var dataPoints []DataPoint

	for i := 0; i < len(rows); i++ {
		var row = rows[i]
		var domain = row.domain
		var uptime DataPoint
		var ttfb DataPoint
		var ttlb DataPoint

		uptime.Metric = "domain.uptime"
		uptime.Timestamp = timestamp
		uptime.Tags = Tag{domain, row.id}

		ttfb.Metric = "domain.ttfb"
		ttfb.Timestamp = timestamp
		ttfb.Tags = Tag{domain, row.id}

		ttlb.Metric = "domain.ttlb"
		ttlb.Timestamp = timestamp
		ttlb.Tags = Tag{domain, row.id}

		uptime.Value, ttfb.Value, ttlb.Value = traceUrl(domain)
		dataPoints = append(dataPoints, uptime, ttfb, ttlb)
	}
	points, err := json.Marshal(dataPoints)
	if err != nil {
		log.Println(err)
	}
	sendDataToDB(points, len(dataPoints))
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
