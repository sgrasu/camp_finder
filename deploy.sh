#!/bin/bash
gcloud functions deploy ScrapeFromMessage --runtime go111 --env-vars-file .sendgrid.yaml --trigger-topic TEST_TOPIC