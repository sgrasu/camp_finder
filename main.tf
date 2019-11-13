//configure google cloud provider
provider "google" {
  credentials = "${file(var.cred)}}"
  project = "${var.var_project}"
  region = "us-west2"
}
