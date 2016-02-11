// infping.go copyright Tor Hveem
// License: MIT

package main

import (
	"bufio"
	"fmt"
	"github.com/influxdata/influxdb/client/v2"
	"github.com/pelletier/go-toml"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func ferr(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func perr(err error) {
	if err != nil {
		fmt.Println(err)
	}
}

func slashSplitter(c rune) bool {
	return c == '/'
}

func readPoints(config *toml.TomlTree, con client.Client) {
	args := []string{"-B 1", "-D", "-r0", "-O 0", "-Q 10", "-p 1000", "-l"}
	hosts := config.Get("hosts.hosts").([]interface{})

	for _, v := range hosts {
		host, _ := v.(string)
		args = append(args, host)
	}

	if _, err := os.Stat("/usr/bin/fping"); os.IsNotExist(err) {
		ferr(err)
	}

	log.Printf("Going to ping the following hosts: %q", hosts)

	cmd := exec.Command("/usr/bin/fping", args...)

	stdout, err := cmd.StdoutPipe()
	ferr(err)

	stderr, err := cmd.StderrPipe()
	ferr(err)

	buff := bufio.NewScanner(stderr)
	go func() {
		for buff.Scan() {
			text := buff.Text()
			fields := strings.Fields(text)

			// Ignore timestamp
			if len(fields) > 1 {
				host := fields[0]
				data := fields[4]
				dataSplitted := strings.FieldsFunc(data, slashSplitter)

				// Remove ,
				dataSplitted[2] = strings.TrimRight(dataSplitted[2], "%,")
				sent, recv, lossp := dataSplitted[0], dataSplitted[1], dataSplitted[2]
				min, max, avg := "", "", ""

				// Ping times
				if len(fields) > 5 {
					times := fields[7]
					td := strings.FieldsFunc(times, slashSplitter)
					min, avg, max = td[0], td[1], td[2]
				}

				log.Printf("Host:%s, loss: %s, min: %s, avg: %s, max: %s", host, lossp, min, avg, max)
				writePoints(config, con, host, sent, recv, lossp, min, avg, max)
			}
		}
	}()
	// Launch pinger
	err = cmd.Start()
	perr(err)
	err = cmd.Wait()
	perr(err)
	std := bufio.NewReader(stdout)
	line, err := std.ReadString('\n')
	perr(err)
	log.Printf("stdout:%s", line)
}

func writePoints(config *toml.TomlTree, con client.Client, host string, sent string, recv string, lossp string, min string, avg string, max string) {
	db := config.Get("influxdb.db").(string)
	measurement := config.Get("influxdb.measurement").(string)

	loss, _ := strconv.Atoi(lossp)
	fields := map[string]interface{}{}
	tags := map[string]string{
		"host": host,
	}

	if min != "" && avg != "" && max != "" {
		min, _ := strconv.ParseFloat(min, 64)
		avg, _ := strconv.ParseFloat(avg, 64)
		max, _ := strconv.ParseFloat(max, 64)

		fields = map[string]interface{}{
			"loss": loss,
			"min":  min,
			"avg":  avg,
			"max":  max,
		}
	} else {
		fields = map[string]interface{}{
			"loss": loss,
		}
	}

	// Create new point batch
	bp, _ := client.NewBatchPoints(client.BatchPointsConfig{
		Database:  db,
		Precision: "ns",
	})

	// Create point and add to batch
	pt, err := client.NewPoint(measurement, tags, fields, time.Now())

	perr(err)

	bp.AddPoint(pt)

	// Write batch to connection
	err = con.Write(bp)

	ferr(err)
}

func main() {
	config, err := toml.LoadFile("config.toml")
	if err != nil {
		fmt.Println("Error:", err.Error())
		os.Exit(1)
	}

	host := config.Get("influxdb.host").(string)
	port := config.Get("influxdb.port").(string)
	username := config.Get("influxdb.user").(string)
	password := config.Get("influxdb.pass").(string)

	conf := client.HTTPConfig{
		Addr:     fmt.Sprintf("http://%s:%s", host, port),
		Username: username,
		Password: password,
	}

	con, err := client.NewHTTPClient(conf)

	if err != nil {
		log.Fatal(err)
	}

	defer con.Close()

	log.Printf("Connected to influxdb!")

	readPoints(config, con)
}
