# rudder-load

This repository is used to generate an artificial load to test the RudderStack architecture under intense traffic.

## Usage

The service is designed to be run in a Kubernetes cluster.

It is possible to create multiple replicas of the service to increase the load.

Each replica owns all writeKeys and will send events via multiple go routines using the configured distribution for each source. Each go routine will send events
owning a set of keys. This way we can guarantee in-order delivery of events per writeKey-userId.

## Example configuration

1. Replicas: 3
2. WriteKeys: 3
3. Concurrency: 200 (number of go routines sending messages per replica, aka "producers")
4. Message generators: 10 (number of go routines that will generate events and send them to the **producers**, see point 3)
5. Total users: 100000 (number of unique users that will be used to generate the messages for each replica)
6. Hot user groups: 70,30 (creates equal number of groups of users and defines their distribution for the events sent)
    - Rules:
      
      a. the sum of all the comma separated values must be equal to 100 (percentage)

      b. the percentage of user IDs concentration that will be used to generate the messages.
        
        - Example:  In this case we have 100,000 total users (see point 5) and we are defining 2 hot user groups so we just divide
      100,000 by 2 which gives us 2 groups of 50k users each. The probability of a message being generated for a user
      in the first group is 70% and 30% for the second group.
7. Event types: page,batch(10,0),batch(30,100) (Types of events to be sent based on the event templates)

   - Example: In this case, the program will generate 3 different types of events:
      * page
      * batch with 10 pages
      * batch with 30 pages and 100 track events

8. Hot event types: 60,25,15 (Event types distribution)

    - Rules:

      a. the sum of all the comma separated values must be equal to 100 (percentage)

      b. the percentage of event types concentration that will be used to generate the messages.
      - Example:  In this case we have 3 event types (see point 7) and we are defining 3 hot event types, one for each event type.
      Given `page,batch(10,0),batch(30,100)` and hot event types `60,25,15` we'll have 60% probability to get a `page`,
      25% probability to get a `batch(10,0)` and 15% probability to get a `batch(30,100)`.

9. Batch sizes and hot batch sizes (they would work the same as hot event types but for the batch sizes)


## Adding more event types

To add more event types simply do:
1. add template inside `templates` folder like `page.json.tmpl` with a file name that starts with `"custom"` E.g. `custom-page.json.tmpl`
2. the name of the file without extension can now be used as an event type E.g. `custom-page`
3. in order to be able to use the new event type one needs to build the rudder-load image that contains the change and use the corresponding image in the values file.

## Ways to Run Load Tests

There are two primary approaches to run load tests with this tool:
1. Using the Makefile (traditional approach)
2. Using the load-runner (more flexible approach with configuration options)

### Method 1: Using the Makefile

In order to deploy you'll have to use the `Makefile` recipes.

The `Makefile` has 2 important variables:
* `K8S_NAMESPACE`: the Kubernetes namespace where it should deploy the `rudder-load` service
* Values file prefix: The prefix of the values file to be used for the deployment. Before deploying you will have to create a copy of your value file (e.g. `http_values.yaml`) and add `_copy.yaml` at the
end of the file name (e.g. `http_values_copy.yaml`). The `Makefile` will use the copied file.
Also, that file is ignored by git so you can add whatever you want to it.

Those are the only variables that you can tune via the `Makefile`.

The docker image is built in the CI pipeline.
If you want you can still build your own image by doing `make DOCKER_USER=<your-docker-username> build`.

! Remember to update your values file (e.g. `http_values_copy.yaml`) with the new image tag (see
`deployment.image`).

#### Examples

```shell
# To deploy
make K8S_NAMESPACE=my-ns deploy-http

# To remove the last deployment
K8S_NAMESPACE=my-ns make delete-http

# To follow the rudder-load logs
K8S_NAMESPACE=my-ns make logs
```

### Method 2: Using the load-runner

The load-runner is a more flexible tool that allows for dynamic configuration and running various load test scenarios. It can be run in two main ways:
1. With command-line flags
2. With a test configuration file

#### Building the load-runner

First, build the load-runner tool:

```shell
go build -o load-runner ./cmd/load-runner
```

#### Option 1: Run with command-line flags

```shell
./load-runner -d <duration> -n <namespace> -l <values-file-prefix> -e CONCURRENCY=500

# Example
./load-runner -d 1m -n rudder-load -l http
```

#### Option 2: Run with a test configuration file

For more complex load test scenarios with multiple phases, you can use a YAML configuration file.

1. Create a test config YAML file:

```yaml
# artifacts/helm/<load-name>_values_copy.yaml will be used
name: http                # Values file prefix (http_values_copy.yaml)
namespace: your-namespace # Kubernetes namespace for deployment
env:                      # Global environment variables for all phases
  MESSAGE_GENERATORS: "200"
  MAX_EVENTS_PER_SECOND: "20000"
phases:                   # Test phases with different configurations
  - duration: 30s         # Phase 1: Run for 30 seconds
    replicas: 1           # With 1 replica
  - duration: 30s         # Phase 2: Run for another 30 seconds
    replicas: 2           # With 2 replicas
    env:                  # Override environment variables for this phase
      MESSAGE_GENERATORS: "300"
      CONCURRENCY: "600"
  - duration: 30s         # Phase 3: Final 30 seconds
    replicas: 1           # Back to 1 replica
```

2. Run the load-runner with the test config file:

```shell
./load-runner -t <path-to-test-config-file>

# Example
./load-runner -t tests/spike.test.yaml
```

#### Load-runner flags

The load-runner supports the following command-line flags:

- `-d`: duration to run (e.g., 1h, 30m, 5s)
- `-n`: namespace where the load runner will be deployed
- `-l`: values file prefix (e.g., for "http", it will use "http_values_copy.yaml")
- `-f`: path to the chart files (default: artifacts/helm)
- `-t`: path to the test config file
- `-e`: environment variables in KEY=VALUE format (can be used multiple times)

#### Configuration Options

The load-runner uses the values from your `<prefix>_values_copy.yaml` file (e.g., `http_values_copy.yaml`). Below is a sample configuration with detailed comments explaining each field:

```yaml
env:
  # The mode of operation, typically "http"
  MODE: "http"

  # Unique identifier for the load test (if empty, a random UUID will be generated)
  LOAD_RUN_ID: "loadRunID1"

  # Number of go routines sending messages per replica (producers)
  CONCURRENCY: "4000"

  # Number of go routines that generate events and send them to producers
  MESSAGE_GENERATORS: "1000"

  # Rate limiting for events (set to 0 for no limit)
  MAX_EVENTS_PER_SECOND: "60000"

  # Source and User Configuration

  # Comma-separated list of write keys
  # These are the write keys used to send events to RudderStack
  SOURCES: "writeKey1,writeKey2"

  # Percentage distribution across sources (must sum to 100)
  # This controls how traffic is distributed across the write keys
  HOT_SOURCES: "60,40"

  # Total number of unique users to simulate
  TOTAL_USERS: "10000"

  # Percentage distribution of user traffic (must sum to 100)
  # For example: "70,30" means 70% of traffic goes to the first user group
  # and 30% to the second user group
  HOT_USER_GROUPS: "70,30"

  # Event Configuration

  # Comma-separated list of event types to generate
  # Options include: track, page, identify, and custom types
  EVENT_TYPES: "track,page,identify"

  # Percentage distribution of event types (must sum to 100)
  # Maps 1:1 with EVENT_TYPES, determining frequency of each type
  HOT_EVENT_TYPES: "50,40,10"

  # Comma-separated list of batch sizes
  BATCH_SIZES: "1,2,3"

  # Percentage distribution of batch sizes (must sum to 100)
  # Controls how frequently each batch size is used
  HOT_BATCH_SIZES: "33,33,34"

  # HTTP Settings

  # Enable/disable HTTP compression
  HTTP_COMPRESSION: "true"

  # HTTP read timeout duration
  HTTP_READ_TIMEOUT: "5s"

  # HTTP write timeout duration
  HTTP_WRITE_TIMEOUT: "5s"

  # Maximum idle connection time
  HTTP_MAX_IDLE_CONN: "1h"

  # Maximum connections per host
  HTTP_MAX_CONNS_PER_HOST: "200000"

  # HTTP concurrency setting
  HTTP_CONCURRENCY: "200000"

  # Content type for HTTP requests
  HTTP_CONTENT_TYPE: "application/json"

  # Target endpoint URL where events will be sent
  HTTP_ENDPOINT: "https://dataplane.rudderstack.com/v1/batch"

  # Other Settings

  # Whether to use one client per slot
  USE_ONE_CLIENT_PER_SLOT: "true"

  # Enable memory usage limitation
  ENABLE_SOFT_MEMORY_LIMIT: "true"

  # Memory limit if ENABLE_SOFT_MEMORY_LIMIT is true
  SOFT_MEMORY_LIMIT: "256mb"

  # Automatically define CPU, memory, message generators and concurrency based on the target event rate set in `MAX_EVENTS_PER_SECOND`
  RESOURCE_CALCULATION: "auto"

  # Automatically define CPU, memory, message generators and concurrency based on the target event rate set in `MAX_EVENTS_PER_SECOND`
  # overprovisioning each resource by the percentage mentioned after the comma e.g. "overprovision,20" means overprovision by 20%
  RESOURCE_CALCULATION: "overprovision,20"

  # Timeout when attempting to port forward 
  PORT_FORWARDING_TIMEOUT: "10s"
```

#### Overriding Configuration

You can override configuration in several ways:

1. **Using command-line flags**: Use the `-e` flag to override specific environment variables:
   ```shell
   ./load-runner -d 5m -n my-namespace -l http -e CONCURRENCY=500 -e MAX_EVENTS_PER_SECOND=1000
   ```

2. **Using a test configuration file**: Define overrides in the `env` section:
   ```yaml
   name: http
   namespace: load-test
   env:
     CONCURRENCY: "500"
     MAX_EVENTS_PER_SECOND: "1000"
   phases:
     - duration: 1m
       replicas: 2
   ```

3. **Using a .env file**: Create a `.env` file in the root directory with the desired overrides:
   ```sh
   SOURCES="writeKey1,writeKey2"
   HTTP_ENDPOINT="https://dataplane.rudderstack.com/v1/batch"
   ```

> [!IMPORTANT]
> For security reasons, sensitive information like `SOURCES` and `HTTP_ENDPOINT` should never be included in test YAML files that might be committed to version control. Always configure these values using the `.env` file or command-line flags.


#### Example: Running a Simple Load Test

Here's an example of running a simple load test for 5 minutes:

```shell
# Build the load-runner
go build -o load-runner ./cmd/load-runner

# Run a load test for 5 minutes
./load-runner -d 5m -n my-namespace -l http -e HTTP_ENDPOINT="https://dataplane.rudderstack.com/v1/batch" -e EVENT_TYPES: "track,page,identify"
```

#### Example: Running a Multi-Phase Load Test

For a test that gradually increases and then decreases load:

1. Create a test configuration file named `escalating.test.yaml`:
   ```yaml
   name: http
   namespace: load-test
   env:
     EVENT_TYPES: "track,page,identify"
     MAX_EVENTS_PER_SECOND: "5000"
   phases:
     - duration: 2m
       replicas: 1
     - duration: 3m
       replicas: 2
     - duration: 5m
       replicas: 4
       env:
         MAX_EVENTS_PER_SECOND: "10000"
     - duration: 3m
       replicas: 2
     - duration: 2m
       replicas: 1
   ```

2. Run the test:
   ```shell
   ./load-runner -t escalating.test.yaml
   ```

This will create a 15-minute load test that follows the pattern: light load (2m) → medium load (3m) → heavy load (5m) → medium load (3m) → light load (2m), with appropriate adjustments to the configuration at each phase.

## Reporting Configuration

The load test tool supports reporting metrics to a monitoring system. This is configured in the test configuration file under the `reporting` section:

```yaml
reporting:
  namespace: mimir          # Namespace for mimir
  interval: 30s             # How often to report metrics
  metrics:
    - name: rps             # Name of the metric to report
```

Currently supported metrics:
- `rps`: Reports the requests per second being sent to the target endpoint

The reporting feature allows you to monitor the load test performance in real-time and analyze the results after the test completes.

### Adding Custom Queries for Reporting Metrics

The load test tool allows you to add custom queries for reporting metrics. This can be configured in the test configuration file under the `reporting` section. You can specify custom queries for metrics by adding a `query` field to the metric definition.

Example configuration with custom queries:

```yaml
reporting:
  namespace: mimir          # Namespace for mimir
  interval: 30s             # How often to report metrics
  metrics:
    - name: rps             # Name of the metric to report
    - name: errors          # Custom metric for error rate
      query: sum(rate(rudder_load_publish_error_rate_total[1m]))
```

In this example, a custom query is added for the `errors` metric, which calculates the error rate using the Prometheus query language. This allows for more detailed and specific monitoring of the load test performance.

Refer to [tests/reporting.test.yaml](tests/reporting.test.yaml) for an example of how to configure custom queries in a test configuration file.

#### Metrics Storage

All collected metrics are stored in JSON files with the following structure:

```json
[
  {
    "timestamp": "2024-03-21T10:30:00Z",
    "metrics": [
      {
        "key": "rps",
        "value": 100
      },
      {
        "key": "errors",
        "value": 5
      }
    ]
  }
]
```

#### Metrics File Location

Metrics files are stored in a `metrics_reports` directory in the current working directory. Each load test generates a unique file named:

```
{test-name}_metrics_{timestamp}.json
```

For example: `http_metrics_20240321_103000.json`