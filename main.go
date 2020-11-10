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
	"log"
	"net"
	"os"
	"strconv"
	"time"

	"golang.org/x/crypto/ssh"
)

const version string = "0.01"
const privatesshkey string = "/etc/NDSmonitor/id_rsa"

type config struct {
	username    string
	password    string
	ndshostname string
	ndsip       net.IP
	sshkey      bool
	refresh     int
}

type users struct {
	id         int64            `json:"id"`
	ipaddress  net.IP           `json:"ip"`
	macaddress net.HardwareAddr `json:"mac"`
	startTime  int64            `json:"added"`
	activeTime int64            `json:"active"`
	duration   int64            `json:"duration"`
	token      string           `json:"token"`
	state      string           `json:"state"`
	download   int64            `json:"downloaded"`
	upload     int64            `json:"uploaded"`
}

type status struct {
	clientlength string `json:"client_list_length"`
	clients      map[string]users
}

// Get values from the Enviroment and set the config.
func startup() config {
	var s config

	log.Printf("NDS Monitor version: %s Starting up. \n", version)

	// Check for the remote host being set
	if os.Getenv("NDShost") == "" {
		log.Fatal("No NDS Host specified")
	} else {
		s.ndshostname = fmt.Sprintf("%s:22", os.Getenv("NDShost"))
	}

	// Check for the ssh username
	if os.Getenv("username") == "" {
		log.Fatal("No ssh username specified")
	} else {
		s.username = os.Getenv("username")
	}

	// FIX this when it's not 12:30am
	if os.Getenv("Polltime") == "" {
		log.Fatal("No Poll Rate specified")
	} else {
		refresh, err := strconv.Atoi(os.Getenv("Polltime"))
		if err != nil {
			log.Fatal("Bad Polling time")
		}
		s.refresh = refresh
	}

	// Check for a password if it's not set then we check to see if the SSH key file is present.
	if os.Getenv("password") == "" {
		if fileExists(privatesshkey) {
			s.sshkey = true
		} else {
			log.Fatal("No password set or SSH key present")
		}
	} else {
		s.password = os.Getenv("password")
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

/*
Commenting this out for now. I need to figure-out how to do SSH keys.
------
func publicKey(path string) (ssh.AuthMethod, error) {
	key, err := ioutil.ReadFile(path)
	if err != nil {
		panic(err)
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		panic(err)
	}
	return ssh.ParsePublicKey(signer)
}

func connectSSH(s config) conn {
	config := &ssh.ClientConfig{
		User: s.username,
		Auth: []ssh.AuthMethod{
			publicKey(privatesshkey)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	conn, err := ssh.Dial("tcp", s.ndshostname, config)
	defer conn.Close()

	return conn
}
*/

// Connect to the server with a password
func connectToHost(user, password, host string) (*ssh.Client, error) {
	sshConfig := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{ssh.Password(password)},
	}

	sshConfig.HostKeyCallback = ssh.InsecureIgnoreHostKey()

	client, err := ssh.Dial("tcp", host, sshConfig)
	if err != nil {
		return nil, err
	}

	return client, nil
}

func main() {
	settings := startup()
	fmt.Println("Startup Successful! Beginning monitoring loop")

	fmt.Printf("Going to use username: %s\n", settings.username)
	if settings.sshkey {
		fmt.Println("Using SSH Private Key")
	} else {
		fmt.Printf("Using this password: %s\n", settings.password)
	}

	client, err := connectToHost(settings.username, settings.password, settings.ndshostname)
	if err != nil {
		log.Fatal(err)
	}

	//var ndsusers []users

	ticker := time.NewTicker(time.Second * time.Duration(settings.refresh))

	for range ticker.C {
		/*  FIX this when it's not 12:30am
		if the session errors, we are going to close the client So I need to figure-out if I want to move the
		connect into the ticker for loop.
		*/
		session, err := client.NewSession()
		if err != nil {
			client.Close()
			log.Println(err)
			log.Println("Reconnecting next refresh interval")
			continue
		}

		var current status

		output, err := session.CombinedOutput("ndsctl json")
		if err != nil {
			panic(err)
		}

		err = json.Unmarshal([]byte(output), &current)
		if err != nil {
			log.Fatal(err)
		}

		fmt.Println("Going to print")

		for key, value := range current.clients {
			fmt.Printf("Key: %s\n", key)
			fmt.Printf("Value: %s\n", value)
			break
		}

		/*
			for x := range current {
				fmt.Println(current[x])
			}
			// fmt.Println(string(output))
		*/
	}
	client.Close()

}
