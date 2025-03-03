# rudder-load

This repository is used to generate an artificial load to test the RudderStack architecture under intense traffic.

## Usage

The service is designed to be run in a Kubernetes cluster.

It is possible to create multiple replicas of the service to increase the load.

Each replica owns a specific writeKey and will send events via multiple go routines. Each go routine will send events
owning a set of keys. This way we can guarantee in-order delivery of events per writeKey-userId.

## Example configuration

1. Replicas: 3
2. WriteKeys: 3
3. Concurrency: 200 (number of go routines sending messages per replica, aka "producers")
4. Message generators: 10

   a. number of go routines that will generate events and send them to the **producers** (see point 3)
5. Total users: 100000

   a. number of unique users that will be used to generate the messages
6. Hot user groups: 70,30

   a. the sum of all the comma separated values must be equal to 100 (percentage)

   b. the percentage of user IDs concentration that will be used to generate the messages.
      - In this case we have 100,000 total users (see point 5) and we are defining 2 hot user groups so we just divide
      100,000 by 2 which gives us 2 groups of 50k users each. The probability of a message being generated for a user
      in the first group is 70% and 30% for the second group.
7. Event types: page,batch(10,0),batch(30,100)

   a. the types of events that will be generated. In this case, the program will generate 3 different types of events:
      * page
      * batch with 10 pages
      * batch with 30 pages and 100 track events
8. Hot event types: 60,25,15

   a. the sum of all the comma separated values must be equal to 100 (percentage)

   b. the percentage of event types concentration that will be used to generate the messages.
      - In this case we have 3 event types (see point 7) and we are defining 3 hot event types, one for each event type.
      Given `page,batch(10,0),batch(30,100)` and hot event types `60,25,15` we'll have 60% probability to get a `page`,
      25% probability to get a `batch(10,0)` and 15% probability to get a `batch(30,100)`.
9. Batches sizes and hot batch sizes (they would work the same as hot event types but for the batch sizes)
10. Custom event types: custom_purchase,custom_add_to_cart

   a. the templates for these event types should be inside the `templates` folder

   b. the event types should start with "custom"


## Adding more event types

To add more event types simply do:
1. add template inside `templates` folder like `page.json.tmpl`
2. the name of the file without extension can now be used as an event type
3. now you have to define a function to populate your template inside `cmd/producer/event_types.go` and then update
   the `var eventGenerators = map[string]eventGenerator{}` map with your function (use name of the template as key)

## How to deploy

In order to deploy you'll have to use the `Makefile` recipes.

The `Makefile` has 2 important variables:
* `K8S_NAMESPACE`: the Kubernetes namespace where it should deploy the `rudder-load` service

Those are the only variables that you can tune via the `Makefile`.
Before deploying you will have to create a copy of your value file (e.g. `http_values.yaml`) and add `_copy.yaml` at the
end of the file name (e.g. `http_values_copy.yaml`). The `Makefile` will use the copied file.
Also, that file is ignored by git so you can add whatever you want to it.

The docker image is built in the CI pipeline.
If you want you can still build your own image by doing `make DOCKER_USER=<your-docker-username> build`.

! Remember to update your values file (e.g. `http_values_copy.yaml`) with the new image tag (see
`deployment.image`).

### Examples

```shell
# To deploy
make K8S_NAMESPACE=my-ns deploy-http

# To remove the last deployment
K8S_NAMESPACE=my-ns make delete-http

# To follow the rudder-load logs
K8S_NAMESPACE=my-ns make logs
```

## Use load runner to generate load for specific values file

### Build the load runner
```shell
go build -o load-runner ./cmd/load-runner
```

### Run the load runner

```sh
./load-runner -d <duration> -n <namespace> -l <values-file-prefix> -e CONCURRENCY=500

# Example
./load-runner -d 1m -n rudder-load -l http
```


### Run the load runner with a test config file

Create a test config yaml file.

```yaml
# artifacts/helm/<load-name>_values_copy.yaml will be used
name: <load-name>
namespace: <namespace>
env:
  MESSAGE_GENERATORS: "200"
  MAX_EVENTS_PER_SECOND: "20000"
phases:
  - duration: 30s
    replicas: 1
  - duration: 30s
    replicas: 2
    env:
      MESSAGE_GENERATORS: "300"
      CONCURRENCY: "600"
  - duration: 30s
    replicas: 1
```

Run the load runner with the test config file.

```shell
./load-runner -t <path-to-test-config-file>

# Example
./load-runner -t tests/spike.test.yaml
```

### Load runner flags

- `-d`: duration to run (e.g., 1h, 30m, 5s)
- `-n`: namespace where the load runner will be deployed
- `-l`: values file prefix
- `-f`: path to the chart files (e.g., artifacts/helm)
- `-t`: path to the test config file
- `-e`: environment variables in KEY=VALUE format (can be used multiple times)
