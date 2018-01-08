package utils

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"github.com/fedesog/webdriver"
	"github.com/woodsaj/chromedriver_har/events"
	"github.com/woodsaj/chromedriver_har/httpArchive"
	"log"
	"net/http"
	"net/http/httptrace"
	"time"
)

type transport struct {
	current *http.Request
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.current = req
	return http.DefaultTransport.RoundTrip(req)
}

var (
	logingPrefs = map[string]webdriver.LogLevel{
		"performance": webdriver.LogInfo,
	}
	chromeDriverPath = "/home/zopnow/work/chromedriver"
	openTSDBurl      = "http://localhost:4242/api/put"
	timeout          = time.Duration(10 * time.Second) //// timeout http get request after 10 seconds
	client           = http.Client{
		Timeout:   timeout,
		Transport: &transport{},
	}
)

type PerfLog struct {
	Method  string                     `json:"method"`
	Params  map[string]json.RawMessage `json:"params"`
	WebView string                     `json:"webview"`
}

func CalculatePageLoadTime(url string) (int64, error) {
	chromeDriver := webdriver.NewChromeDriver(chromeDriverPath)
	err := chromeDriver.Start()
	if err != nil {
		log.Fatal(err)
	}
	defer chromeDriver.Stop()

	desired := webdriver.Capabilities{"loggingPrefs": logingPrefs}
	required := webdriver.Capabilities{}
	session, err := chromeDriver.NewSession(desired, required)
	if err != nil {
		return int64(-1), err
	}
	defer session.Delete()

	_, err = session.Log("performance")
	if err != nil {
		return int64(-1), err
	}

	err = session.Url(url)
	if err != nil {
		return int64(-1), err
	}

	logs, err := session.Log("performance")
	if err != nil {
		return int64(-1), err
	}

	e, err := events.NewFromLogEntries(logs)
	if err != nil {
		return int64(-1), err
	}

	har, err := httpArchive.CreateHARFromEvents(e)
	if err != nil {
		return int64(-1), err
	}
	return har.Log.Pages[0].PageTimings.OnLoad * 1000, nil // converting page load timings to micro second
}

func SendDataToDB(points []byte, numberOfPoints int) {
	req, err := http.NewRequest("POST", openTSDBurl, bytes.NewBuffer(points))
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
		log.Println("Sent", numberOfPoints, " Data Points to OpenTSDB")
	}
}

func SendRequest(url string) (int64, int64, int64, int64, int64) {
	var start, dnsStart, tlsHandshakeStart time.Time
	var ttfb, ttlb, dns, ssl time.Duration
	servicable := int64(1)
	req, _ := http.NewRequest("GET", url, nil)
	trace := &httptrace.ClientTrace{
		GotFirstResponseByte: func() { ttfb = time.Since(start) },
		DNSStart:             func(httptrace.DNSStartInfo) { dnsStart = time.Now() },
		DNSDone:              func(httptrace.DNSDoneInfo) { dns = time.Since(dnsStart) },
		TLSHandshakeStart: func() {
			tlsHandshakeStart = time.Now()
		},
		TLSHandshakeDone: func(tls.ConnectionState, error) {
			ssl = time.Since(tlsHandshakeStart)
		},
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
	start = time.Now()
	resp, err := client.Do(req)
	ttlb = time.Since(start)
	if err != nil {
		servicable = 0
		log.Println("For URl: "+url+" TTFB: ", ttfb, " TTLB: ", ttlb, " DNS: ", dns, " SSL: ", ssl, " Error: ", err)
	} else {
		if resp.StatusCode != 200 {
			servicable = 0
		}
		log.Println("For URl: "+url+" httpStatusCode:", resp.StatusCode, " TTFB: ", ttfb, " TTLB: ", ttlb, " DNS:", dns, " SSL:", ssl, "\n")
	}
	return servicable, int64(ttfb / time.Microsecond), int64(ttlb / time.Microsecond),
		int64(dns / time.Microsecond), int64(ssl / time.Microsecond)
}

// func calculatePageLoadTime(url string) {
// 	// Web driver Start
// 	var start time.Time
// 	var pageLoadTime time.Duration
// 	chromeDriver := webdriver.NewChromeDriver(chromeDriverPath)
// 	err := chromeDriver.Start()
// 	if err != nil {
// 		log.Println(err)
// 	}
// 	defer chromeDriver.Stop()

// 	desired := webdriver.Capabilities{"Platform": "Linux"}
// 	required := webdriver.Capabilities{}
// 	session, err := chromeDriver.NewSession(desired, required)
// 	if err != nil {
// 		log.Println(err)
// 	}
// 	defer session.Delete()
// 	time.Sleep(3 * time.Second) // giving time for browser to open
// 	start = time.Now()
// 	err = session.Url(url)
// 	pageLoadTime = time.Since(start)

// 	// Web driver Stop
// 	log.Println("Webpage page load time", pageLoadTime)
// 	if err != nil {
// 		log.Println(err)
// 	}
// }
