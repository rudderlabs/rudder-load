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
   * it is a good idea to have enough writeKeys for the replicas since replica 0 will use writeKey 0,
     replica 1 will use writeKey 1, etc.
3. Concurrency: 200 (number of go routines sending messages per replica, aka "producers")
4. Message generators: 10
   a. number of go routines that will generate events and send them to the **producers** (see point 3)
5. Total users: 100000
   a. number of unique users that will be used to generate the messages
6. Hot user groups: 70,30
   a. the sum of all the comma separated values must be equal to 100 (percentage)
   b. the percentage of user IDs concentration that will be used to generate the messages. 
      In this case we have 100,000 total users (see point 5) and we are defining 2 hot user groups so we just divide
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
      In this case we have 3 event types (see point 7) and we are defining 3 hot event types, one for each event type.
      Given `page,batch(10,0),batch(30,100)` and hot event types `60,25,15` we'll have 60% probability to get a `page`,
      25% probability to get a `batch(10,0)` and 15% probability to get a `batch(30,100)`.

## Adding more event types

To add more event types simply do:
1. add template inside `templates` folder like `batch.json.tmpl` or `page.json.tmpl`
2. the name of the file without extension can now be used as an event type
3. now you have to define a function to populate your template inside `cmd/producer/event_types.go` and then update
   the `var eventGenerators = map[string]eventGenerator{}` map with your function (use name of the template as key)
4. anything that you pass in the configuration between parenthesis will be passed to the function as the last parameter
   i.e. `n []int`, see `batchFunc` as an example because it uses `n` to know how many pages and tracks it should have

## How to deploy

In order to deploy you'll have to use the `Makefile` recipes.

The `Makefile` has 2 important variables:
* `K8S_NAMESPACE`: the Kubernetes namespace where it should deploy the `rudder-load` service
* `DOCKER_USER`: the Docker user to use to push `rudder-load` Docker image

Those are the only variables that you can tune via the `Makefile`.
Before deploying you will have to create a copy of your value file (e.g. `http_values.yaml`) and add `_copy.yaml` at the 
end of the file name (e.g. `http_values_copy.yaml`). The `Makefile` will use the copied file. 
Also, that file is ignored by git so you can add whatever you want to it.

### Examples

```shell
# To deploy
make K8S_NAMESPACE=my-ns DOCKER_USER=francesco deploy-http

# To remove the last deployment
K8S_NAMESPACE=my-ns make delete

# To follow the rudder-load logs
K8S_NAMESPACE=my-ns make logs
```