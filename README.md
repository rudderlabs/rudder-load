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
3. Concurrency: 200 (number of go routines per replica, we'll call them **slots**)
4. Message generators: 10
   a. number of go routines that will generate events and send them to the **slots** (see point 3)
5. Keys per slot map: 50
   a. number of **unique** keys that each slot will own
   b. this can be a map as well like 10_20_30_40 which would mean that 50 slots will own 10 keys, 
      50 slots will own 20 keys, 50 slots 30 keys and 50 slots 40 keys (because we create 4 groups,
      and we have 200 slots)
6. Traffic distribution percentage: 60_40
   a. 60% of the traffic will be sent to 60% of the slots (i.e. 120 slots)
   b. 40% of the traffic will be sent to 40% of the slots (i.e. 80 slots)

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