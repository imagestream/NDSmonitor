/*
Author: Joshua Snyder
Date:   11-6-2020
Desc:   This program scrapes the ndsctl command and stores the results in a
        influxdb database.
*/
package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v2"

	influxclient "github.com/influxdata/influxdb1-client/v2"
)

const version string = "0.01"
const privatesshkey string = "/etc/NDSmonitor/id_rsa"
const configfile string = "/etc/NDSmonitor/config.yml"

type config struct {
	Username       string
	Password       string
	Ndshostname    string
	Name           string
	ndsip          net.IP
	sshkey         bool
	Refresh        int
	InfluxdbServer string
	InfluxDB       string
	InfluxUsername string
	InfluxPassword string
}

type users struct {
	ID         json.Number `json:"id"`
	Ipaddress  string      `json:"ip"`
	Macaddress string      `json:"mac"`
	StartTime  json.Number `json:"added"`
	ActiveTime json.Number `json:"active"`
	Duration   json.Number `json:"duration"`
	Token      string      `json:"token"`
	State      string      `json:"state"`
	Download   json.Number `json:"downloaded"`
	Upload     json.Number `json:"uploaded"`
}

type status struct {
	Clientlength string           `json:"client_list_length"`
	Clients      map[string]users `json:"clients"`
}

// Get values from the Enviroment and set the config.
func startup() config {
	var s config

	log.Printf("NDS Monitor version: %s Starting up. \n", version)

	// Look for the config file if it's pressent use it and not ENV
	if fileExists(configfile) {
		source, err := ioutil.ReadFile(configfile)
		if err != nil {
			log.Fatal(err)
		}

		err = yaml.Unmarshal(source, &s)
		if err != nil {
			log.Fatal(err)
		}
	} else {
		// Check for the remote host being set
		if os.Getenv("NDShost") == "" {
			log.Fatal("No NDS Host specified")
		} else {
			s.Ndshostname = fmt.Sprintf("%s:22", os.Getenv("NDShost"))
		}

		// Check for the ssh Username
		if os.Getenv("Username") == "" {
			log.Fatal("No ssh Username specified")
		} else {
			s.Username = os.Getenv("Username")
		}

		// FIX this when it's not 12:30am
		if os.Getenv("Polltime") == "" {
			log.Fatal("No Poll Rate specified")
		} else {
			Refresh, err := strconv.Atoi(os.Getenv("Polltime"))
			if err != nil {
				log.Fatal("Bad Polling time")
			}
			s.Refresh = Refresh
		}

		// Check for a Password if it's not set then we check to see if the SSH key file is present.
		if os.Getenv("Password") == "" {
			if fileExists(privatesshkey) {
				s.sshkey = true
			} else {
				log.Fatal("No Password set or SSH key present")
			}
		} else {
			s.Password = os.Getenv("Password")
		}
	}

	return s
}

// Verify that a file is present.
func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

// Connect to the server with a Password
func connectToHost(user, Password, host string) (*ssh.Client, error) {
	sshConfig := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{ssh.Password(Password)},
	}

	sshConfig.HostKeyCallback = ssh.InsecureIgnoreHostKey()

	client, err := ssh.Dial("tcp", host, sshConfig)
	if err != nil {
		return nil, err
	}

	return client, nil
}

// Probe the remote host and queue the data for influxdb
func probeNDS(dbqueue chan influxclient.BatchPoints, c config, session *ssh.Session) {

	// Setup the Tags we will be using later
	tags := make(map[string]string)
	tags["Captive Portal"] = c.Name

	var current status
	var authenticated int
	var preauth int

	// Allocate a new batch point for this probe
	bp := newInfluxBP(c)

	output, err := session.CombinedOutput("ndsctl json")
	if err != nil {
		panic(err)
	}

	err = json.Unmarshal([]byte(output), &current)
	if err != nil {
		log.Fatal(err)
	}

	for _, value := range current.Clients {
		looptags := make(map[string]string)
		looptags["Captive Portal"] = c.Name
		var skip bool

		if value.State == "Authenticated" {
			authenticated++
		} else {
			preauth++
		}

		// Set the tags for this host. We overwrite these everytime through the loop
		looptags["Mac_Address"] = strings.ToUpper(value.Macaddress)
		looptags["Ip_Address"] = value.Ipaddress
		looptags["ID"] = value.ID.String()
		looptags["Token"] = value.Token
		looptags["Auth"] = value.State

		/*
			The output for these values are in KB so since I want bytes
			like a sane person, I have to convert them and then multiply by
			1024.
		*/
		downBytes, err := strconv.Atoi(value.Download.String())
		if err != nil {
			log.Println(err)
			skip = true
		} else {
			downBytes = downBytes * 1024
		}
		upBytes, err := strconv.Atoi(value.Upload.String())
		if err != nil {
			log.Println(err)
			skip = true
		} else {
			upBytes = upBytes * 1024
		}

		if skip != true && value.State == "Authenticated" {
			queuePointint("Download_Bytes", looptags, downBytes, bp)
			queuePointint("Upload_Bytes", looptags, upBytes, bp)
		}
	}

	queuePointint("Authenticated", tags, authenticated, bp)
	queuePointint("PreAuth", tags, preauth, bp)

	// Send the batchpoints the the Database server
	if len(dbqueue) < 1000 {
		// Queue the batch point to the dbworker queue
		dbqueue <- bp
	} else {
		log.Println("Influx DB Queue Full, not recording sample")
	}
}

func main() {
	settings := startup()
	fmt.Println("Startup Successful! Beginning monitoring loop")

	fmt.Printf("Going to use Username: %s\n", settings.Username)
	if settings.sshkey {
		fmt.Println("Using SSH Private Key")
	} else {
		fmt.Printf("Using this password: %s\n", settings.Password)
	}

	client, err := connectToHost(settings.Username, settings.Password, settings.Ndshostname)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	// create a channel for Influx messages. And start the db Worker.
	DbQueue := make(chan influxclient.BatchPoints, 1000)
	go dbWorker(settings, DbQueue)

	ticker := time.NewTicker(time.Second * time.Duration(settings.Refresh))

	for range ticker.C {
		/*  FIX this when it's not 12:30am
		if the session errors, we are going to close the client So I need to figure-out if I want to move the
		connect into the ticker for loop.
		*/
		session, err := client.NewSession()
		if err != nil {
			client.Close()
			log.Println(err)
			log.Println("Reconnecting next Refresh interval")
			continue
		}
		probeNDS(DbQueue, settings, session)
	}
}
