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
	"log/syslog"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v2"

	influxclient "github.com/influxdata/influxdb1-client/v2"
)

const version string = "0.05"
const privatesshkey string = "/etc/NDSmonitor/id_rsa"
const configfile string = "/etc/NDSmonitor/config.yml"

type config struct {
	Username       string
	Password       string
	Ndshostname    string
	Name           string
	ndsip          net.IP
	sshkey         bool
	SelfMonitor    bool
	Refresh        int
	InfluxdbServer string
	InfluxDB       string
	InfluxUsername string
	InfluxPassword string
	AllowRestart   bool
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

	logwriter, err := syslog.New(syslog.LOG_NOTICE, "NDSmonitor")
	if err == nil {
		log.SetOutput(logwriter)
	}

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

/*
Restart Nds on the remote system:
Currently we have a hardwired location for the init script not sure if this
will ever be different from this default so leaving it like this for now.
*/
func restartNDS(c config, session *ssh.Session) error {
	output, err := session.CombinedOutput("/etc/init.d/nodogsplash restart")
	if err != nil {
		log.Println(err)
		return err
	}
	log.Println(output)
	return nil
}

// Probe the remote host and queue the data for influxdb
func probeNDS(dbqueue chan influxclient.BatchPoints, c config, session *ssh.Session) {
	// Lets see how long the probe takes to run. So we can identify if we are having long probe times
	startProbe := time.Now()

	// Setup the Tags we will be using later
	tags := make(map[string]string)
	tags["Captive Portal"] = c.Name

	var current status
	var authenticated int
	var preauth int
	var ndsError bool

	// Allocate a new batch point for this probe
	bp := newInfluxBP(c)

	output, err := session.CombinedOutput("ndsctl json")
	if err != nil {
		log.Println(err)
		ndsError = true
	}

	// Check if we got output indcating Nodogsplash isn't running and raise an error if it's not.
	if string(output) == "ndsctl: nodogsplash probably not started (Error: Connection refused)" {
		log.Println("NoDogSplash is not running on remote system.")
		ndsError = true
	}

	// If we got an error above restart the NDS on the remote Opuntia/OpenWrt system
	if ndsError && c.AllowRestart {
		restartNDS(c, session)
		if err != nil {
			log.Println(err)
		}
	}

	err = json.Unmarshal([]byte(output), &current)
	if err != nil {
		log.Println(err)
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
			The output for these values are in KB. So since I want bytes
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

		// We don't log any information for unauthenticated clients
		if skip != true && value.State == "Authenticated" {
			queuePointint("Download_Bytes", looptags, downBytes, bp)
			queuePointint("Upload_Bytes", looptags, upBytes, bp)
		}
	}

	if ndsError {
		queuePointint("NdsError", tags, 1, bp)
	} else {
		queuePointint("Authenticated", tags, authenticated, bp)
		queuePointint("PreAuth", tags, preauth, bp)
		queuePointint("NdsError", tags, 0, bp)
	}

	probeDuration := time.Since(startProbe)
	if c.SelfMonitor {
		queuePointint("ProbeTime", tags, int(time.Since(startProbe)), bp)
	}

	if probeDuration >= time.Duration(180)*time.Second {
		log.Printf("Probe Duration took : %s", probeDuration)
	}

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

	client, err := connectToHost(settings.Username, settings.Password, settings.Ndshostname)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	// create a channel for Influx messages. And start the db Worker.
	DbQueue := make(chan influxclient.BatchPoints, 1000)
	go dbWorker(settings, DbQueue)

	ticker := time.NewTicker(time.Second * time.Duration(settings.Refresh))

	var active int

	for range ticker.C {
		active++

		mark := active % 30

		if mark == 0 {
			log.Println(" Still active")
		}

		/*
			First try on the reconnection logic. If we can't create a new session.
			We close the current session. And then try to connect again.
			I am going to need to see failure in connectivity to see if this works.
		*/
		session, err := client.NewSession()
		if err != nil {
			client.Close()
			log.Println(err)
			log.Println("Attempting to reconnect")
			client, err = connectToHost(settings.Username, settings.Password, settings.Ndshostname)
			if err != nil {
				log.Println(err)
			}
			continue
		}
		go probeNDS(DbQueue, settings, session)
	}
}
