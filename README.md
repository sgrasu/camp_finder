# CampFinder

---

CampFinder is a cloud function which scrapes the recreation.gov website for campsite availabilite. Intended use is for a [cloud scheduler](https://cloud.google.com/scheduler/) Cronjob to publish a message to a [pub/sub](https://cloud.google.com/pubsub/) topic to which the cloud function is subscribed.
