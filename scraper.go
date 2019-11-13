// Copyright 2019 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// [START functions_helloworld_pubsub]
// [START functions_helloworld_background]

// Package scraper provides functions to scrape recreation.gov for campsite availability
package scraper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cloud.google.com/go/pubsub"
	scheduler "cloud.google.com/go/scheduler/apiv1"
	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
	"google.golang.org/api/iterator"
	schedulerpb "google.golang.org/genproto/googleapis/cloud/scheduler/v1"
)

const layoutISO = "2006-1-2"

//Campground message
type Campground struct {
	Campsites map[string]Campsite
}

//Campsite campsite
type Campsite struct {
	CampsiteID     int    `json:"campsite_id"`
	CampsiteType   string `json:"campsite_type"`
	Availabilities map[time.Time]string
}

// MessageContent is the payload of a Pub/Sub event.
type MessageContent struct {
	Name       string
	Campground string
	Arrival    string
	Departure  string
}

// ScrapeFromMessage consumes a Pub/Sub message.
func ScrapeFromMessage(ctx context.Context, m pubsub.Message) error {
	messageContent := MessageContent{}
	err := json.Unmarshal([]byte(m.Data), &messageContent)
	if err != nil {
		log.Println(err)
	}
	if len(messageContent.Name) <= 0 {
		return errors.New("this is an error")
	}
	id := messageContent.Campground
	jobName := messageContent.Name
	arrival, _ := time.Parse(layoutISO, messageContent.Arrival)
	departure, _ := time.Parse(layoutISO, messageContent.Departure)
	available := ScrapeAvailability(id, arrival, departure)
	if len(available) > 0 {
		sendEmail(id, available, arrival.Format("Mon Jan 2"), departure.Format("Mon Jan 2"))
		deleteJob(jobName)
	}
	return nil
}

//TestPub is just an example of publishing to google pub/sub
func TestPub(available []string) error {
	projectID := "camp-finder-258618"
	topicID := "TEST_PUB_TOPIC"
	ctx := context.Background()
	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		return fmt.Errorf("pubsub.NewClient: %v", err)
	}

	var wg sync.WaitGroup
	var totalErrors uint64
	t := client.Topic(topicID)

	result := t.Publish(ctx, &pubsub.Message{
		Data: []byte("Found these " + strings.Join(available, ",")),
	})
	wg.Add(1)
	go func(res *pubsub.PublishResult) {
		defer wg.Done()
		// The Get method blocks until a server-generated ID or
		// an error is returned for the published message.
		_, err := res.Get(ctx)
		if err != nil {
			// Error handling code can be added here.
			log.Println(fmt.Sprintf("Failed to publish: %v", err))
			atomic.AddUint64(&totalErrors, 1)
			return
		}
	}(result)
	wg.Wait()
	if totalErrors > 0 {
		return fmt.Errorf(" messages did not publish successfully")
	}
	return nil
}

//ScrapeAvailability scrape recreation.gov for the campground and dates specified
func ScrapeAvailability(campgroundID string, arrival time.Time, departure time.Time) []string {
	firstOfMonth := time.Date(arrival.Year(), arrival.Month(), 1, 0, 0, 0, 0, time.UTC)
	url := fmt.Sprintf("https://www.recreation.gov/api/camps/availability/campground/%s/month?start_date=%s",
		campgroundID, firstOfMonth.Format("2006-01-02T15:04:05.999999Z"))
	response, _ := http.Get(url)
	campground := Campground{}
	data, _ := ioutil.ReadAll(response.Body)
	json.Unmarshal([]byte(data), &campground)
	availableSites := getAvailableSites(campground, arrival, departure)
	return availableSites
}

func getAvailableSites(campground Campground, arrival time.Time, departure time.Time) []string {
	dates := getDates(arrival, departure)

	count := 0
	campsiteNames := []string{}
	for siteID, site := range campground.Campsites {
		for idx, date := range dates {
			if site.Availabilities[date] != "Available" {
				break
				//fmt.Println(id + " site available on " + date.Format("Mon Jan 2"))
			} else if idx == len(dates)-1 {
				count++
				campsiteNames = append(campsiteNames, siteID)
			}
		}
	}
	return campsiteNames
}

func getDates(startDate time.Time, endDate time.Time) []time.Time {
	dates := []time.Time{}

	for day := 0; day < int(endDate.Sub(startDate).Hours()/24); day++ {
		hours := fmt.Sprintf("%dh", 24*day)
		dayDuration, _ := time.ParseDuration(hours)
		dates = append(dates, startDate.Add(dayDuration))
	}
	return dates

}

func sendEmail(id string, availableSites []string, arrival string, departure string) {
	from := mail.NewEmail(" Stefan", "stefan@stefangrasu.com")
	subject := fmt.Sprintf("Available sites found for %s between %s and %s", id, arrival, departure)
	to := mail.NewEmail("Stefan", "sgrasu17@gmail.com")
	plainTextContent := "and easy to do anywhere, even with Go"
	htmlContent := "<strong>" + "Found these available sites: " + strings.Join(availableSites, ",") + "</strong>"
	message := mail.NewSingleEmail(from, subject, to, plainTextContent, htmlContent)
	client := sendgrid.NewSendClient(os.Getenv("SENDGRID_API_KEY"))
	response, err := client.Send(message)
	if err != nil {
		log.Println(err)
	} else {
		fmt.Println(response.StatusCode)
		fmt.Println(response.Body)
		fmt.Println(response.Headers)
	}
}

func logJobs() error {
	ctx := context.Background()
	c, err := scheduler.NewCloudSchedulerClient(ctx)
	if err != nil {
		log.Println("Failed to list jobs: ", err)
	}

	req := &schedulerpb.ListJobsRequest{
		Parent: "projects/camp-finder-258618/locations/us-west2",
	}
	it := c.ListJobs(ctx, req)
	for {
		_, err := it.Next()
		if err == iterator.Done {
			return nil
		}
		if err != nil {
			log.Println("whyhwy2", err)
			return err
		}
		//log.Println(resp.Description)
	}
	return nil
}

func deleteJob(jobName string) error {
	//log.Println("gonna try and delte")
	ctx := context.Background()
	c, err := scheduler.NewCloudSchedulerClient(ctx)
	if err != nil {
		log.Println("error cuz", err)
		return err
	}

	req := &schedulerpb.DeleteJobRequest{
		Name: fmt.Sprintf("projects/camp-finder-258618/locations/us-west2/jobs/%s", jobName),
	}
	err = c.DeleteJob(ctx, req)
	if err != nil {
		log.Println("didn't delete because", err)
		return err
	}
	return nil
}
