package main

import (
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	"cloud.google.com/go/datastore"
	"github.com/jasonlvhit/gocron"
)

type DataLoad struct {
	LastCommunicationTime time.Time `json:"last_communication_time"`
	InstantPower          float64   `json:"instant_power"`
	InstantReactivePower  float64   `json:"instant_reactive_power"`
	InstantApparentPower  float64   `json:"instant_apparent_power"`
	Frequency             float64   `json:"frequency"`
	EnergyExported        float64   `json:"energy_exported"`
	EnergyImported        float64   `json:"energy_imported"`
	InstantAverageVoltage float64   `json:"instant_average_voltage"`
	InstantTotalCurrent   float64   `json:"instant_total_current"`
	IACurrent             float64   `json:"i_a_current"`
	IBCurrent             float64   `json:"i_b_current"`
	ICCurrent             float64   `json:"i_c_current"`
}
type BatteryOutputData struct {
	DataPointTime time.Time
	Site          DataLoad `json:"site"`
	Battery       DataLoad `json:"battery"`
	Load          DataLoad `json:"load"`
	Solar         DataLoad `json:"solar"`
	Busway        DataLoad `json:"busway"`
	Frequency     DataLoad `json:"frequency"`
}

var ctx = context.Background()
var projectID = os.Getenv("projectID")
var url = fmt.Sprintf("http://%v/api/meters/aggregates", os.Getenv("PW2IP"))
var interval uint64 = 5

func main() {
	gob.Register(BatteryOutputData{})
	gocron.Every(interval).Seconds().Do(task)
	<-gocron.Start()
}

func task() {

	clientDataStore, err := datastore.NewClient(ctx, projectID)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	spaceClient := http.Client{
		Timeout: time.Second * 2, // Maximum of 2 secs
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		log.Fatal(err)
	}

	res, getErr := spaceClient.Do(req)
	if getErr != nil {
		log.Fatal(getErr)
	}

	body, readErr := ioutil.ReadAll(res.Body)
	if readErr != nil {
		log.Fatal(readErr)
	}

	outPutData := BatteryOutputData{}

	jsonErr := json.Unmarshal(body, &outPutData)
	if jsonErr != nil {
		log.Fatal(jsonErr)
	}

	outPutData.DataPointTime = time.Now()

	dataKey := datastore.IncompleteKey("BatteryOutputData", nil)

	if _, err := clientDataStore.Put(ctx, dataKey, &outPutData); err != nil {
		log.Fatalf("Failed to save data entry: %v", err)
	}
}
