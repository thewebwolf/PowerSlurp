package main

import (
	"context"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	"cloud.google.com/go/datastore"
	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/jasonlvhit/gocron"

	"github.com/dgrijalva/jwt-go"
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
var interval uint64 = 60

var f MQTT.MessageHandler = func(client MQTT.Client, msg MQTT.Message) {
	fmt.Printf("TOPIC: %s\n", msg.Topic())
	fmt.Printf("MSG: %s\n", msg.Payload())
}

const (
	// For simplicity these files are in the same folder as the app binary.
	// You shouldn't do this in production.
	privKeyPath = "rsa_private.pem"
	pubKeyPath  = "rsa_cert.pem"
)

var (
	verifyKey *rsa.PublicKey
	signKey   *rsa.PrivateKey
)

func fatal(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func initKeys() {
	signBytes, err := ioutil.ReadFile(privKeyPath)
	fatal(err)

	signKey, err = jwt.ParseRSAPrivateKeyFromPEM(signBytes)
	fatal(err)

	verifyBytes, err := ioutil.ReadFile(pubKeyPath)
	fatal(err)

	verifyKey, err = jwt.ParseRSAPublicKeyFromPEM(verifyBytes)
	fatal(err)
}

func NewTlsConfig() *tls.Config {

	// Import client certificate/key pair
	cert, err := tls.LoadX509KeyPair("samplecerts/client-crt.pem", "samplecerts/client-key.pem")
	if err != nil {
		panic(err)
	}

	// Just to print out the client certificate..
	cert.Leaf, err = x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		panic(err)
	}

	// Create tls.Config with desired tls properties
	return &tls.Config{
		// RootCAs = certs used to verify server cert.
		RootCAs: nil,
		// ClientAuth = whether to request cert from server.
		// Since the server is set up for SSL, this happens
		// anyways.
		ClientAuth: tls.NoClientCert,
		// ClientCAs = certs used to validate client cert.
		ClientCAs: nil,
		// InsecureSkipVerify = verify that cert contents
		// match server. IP matches what is in cert etc.
		InsecureSkipVerify: true,
		// Certificates = list of certs client sends to server.
		Certificates: []tls.Certificate{cert},
	}
}

func GetMQTTClient() MQTT.Client {
	claims := &jwt.StandardClaims{
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Unix() + 3600,
		Audience:  "powerchat-187002",
	}

	tokenb := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	ss, _ := tokenb.SignedString(signKey)

	//create a ClientOptions struct setting the broker address, clientid, turn
	//off trace output and set the default message handler

	tlsconfig := NewTlsConfig()

	opts := MQTT.NewClientOptions()
	opts.AddBroker("ssl://mqtt.googleapis.com:8883")
	opts.SetTLSConfig(tlsconfig)
	mqttClientID := fmt.Sprintf("projects/%v/locations/asia-east1/registries/home/devices/dev1", projectID)

	opts.SetClientID(mqttClientID)
	opts.SetUsername("unused")
	opts.SetProtocolVersion(4)
	opts.SetPassword(ss)

	opts.SetDefaultPublishHandler(f)

	//create and start a client using the above ClientOptions
	c := MQTT.NewClient(opts)
	if token := c.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}

	return c
}

func main() {
	initKeys()

	gob.Register(BatteryOutputData{})
	gob.Register(DataLoad{})
	gocron.Every(interval).Seconds().Do(task)
	<-gocron.Start()

}

func task() {
	client := GetMQTTClient()

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

	text := fmt.Sprintf("Current Solar Output %v", outPutData.Solar.InstantPower)
	fmt.Println(text)
	token := client.Publish("projects/powerchat-187002/topics/devicetelemetry", 0, false, text)
	token.Wait()

	client.Disconnect(250)
}
