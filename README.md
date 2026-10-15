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

    a. Running the load locally with Docker Compose (for development and testing)

    b. Running the load remotely on Kubernetes with helm

### Method 1: Using the Makefile

In order to deploy you'll have to use the `Makefile` recipes.

The `Makefile` has 2 important variables:
* `K8S_NAMESPACE`: the Kubernetes namespace where it should deploy the `rudder-load` service
* Values file prefix: The prefix of the values file to be used for the deployment. Before deploying you will have to create a copy of your value file (e.g. `http_values.yaml`) and add `_copy.yaml` at the
end of the file name (e.g. `http_values_copy.yaml`). The `Makefile` will use the copied file.
Also, that file is ignored by git so you can add whatever you want to it.

Optionally, `IMAGE` overrides the image set in the values file (`deployment.image`).

The docker image is built in the CI pipeline and published to ECR as `422074288268.dkr.ecr.us-east-1.amazonaws.com/rudderstack/rudder-load`, tagged `main`, `pr-<number>` and, for each release, `<version>` (e.g. `0.1.0`) and `latest`.
If you want you can still build your own image by doing `make DOCKER_USER=<your-docker-username> build`.

! Remember to update your values file (e.g. `http_values_copy.yaml`) with the image you want (see `deployment.image`), or pass `IMAGE`.

Deploying again upgrades the existing release in place, so there is no need to delete it first.

#### Examples

```shell
# To deploy
make K8S_NAMESPACE=my-ns deploy-http

# To deploy a specific image
make K8S_NAMESPACE=my-ns IMAGE=422074288268.dkr.ecr.us-east-1.amazonaws.com/rudderstack/rudder-load:0.1.0 deploy-http

# To remove the last deployment
K8S_NAMESPACE=my-ns make delete-http

# To follow the rudder-load logs
K8S_NAMESPACE=my-ns make logs
```

### Method 2: Using the load-runner

The load-runner is a more flexible tool that allows for dynamic configuration and running various load test scenarios. It can be run in two main ways:
1. With command-line flags
2. With a test configuration file
3. Using a combination of a test configuration file with values overriden by command-line flags

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
- `-local`: boolean variable that is set to execute the test locally using Docker Compose

#### Configuration Options

The load-runner uses the values from your `<prefix>_values_copy.yaml` file (e.g., `http_values_copy.yaml`). Below is a sample configuration with detailed comments explaining each field:

```yaml
env:
  # The mode of operation: "http", "http2", "pulsar", or "stdout"
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

  # Can be either:
  # 1. A comma-separated list of write keys
  # 2. A single integer N, which will generate N sources with predictable names like "source-0", "source-1", etc.
  # These are the write keys used to send events to RudderStack
  SOURCES: "writeKey1,writeKey2"  # or simply "3" to generate 3 sources

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
  # Options include: track, smaller-track, page, identify, ut-email, and custom types
  # smaller-track: same as track, with a ~3 KB payload per event instead of ~5 KB
  # ut-email: generates events with email fields for testing user transformers that hash emails
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

  # How many times to retry a request after a connection error, in the http and http2 modes: a DNS lookup failure other
  # than "no such host", a dial timeout or refused connection, the connection breaking while the request is sent, or
  # the connection closed before any response byte. The last one can resend a request the server already processed,
  # so its count in rudder_load_retried_requests_count{reason="connection_closed"} is an upper bound on duplicates.
  # Backs off from 100ms to 5s with jitter. 0 disables retries. rudder_load_retried_requests_count counts the retries
  # by reason, and rudder_load_number_of_requests_count{error="true"} counts the requests dropped in the end.
  HTTP_MAX_RETRIES: "10"

  # Pulsar Settings

  # Required Settings

  # Pulsar service URL (e.g., "pulsar://localhost:6650")
  PULSAR_SERVICE_URL: "pulsar://localhost:6650"

  # Topic to produce messages to
  PULSAR_TOPIC: "rudder-events"

  # Optional Client Settings

  # Timeout for operations (default: 30s)
  PULSAR_OPERATION_TIMEOUT: "30s"

  # Timeout for connections (default: 30s)
  PULSAR_CONNECTION_TIMEOUT: "30s"

  # Enable TLS (default: false)
  PULSAR_TLS_ENABLE: "false"

  # Allow insecure TLS connections (default: false)
  PULSAR_TLS_ALLOW_INSECURE: "false"

  # Path to the TLS trust certificates file
  PULSAR_TLS_TRUST_CERTS_FILE_PATH: ""

  # Optional Producer Settings

  # Enable message batching (default: true)
  PULSAR_BATCHING_ENABLED: "true"

  # Maximum number of messages in a batch (default: 1000)
  PULSAR_BATCHING_MAX_MESSAGES: "1000"

  # Maximum size of a batch in bytes (default: 128KB)
  PULSAR_BATCHING_MAX_SIZE: "131072"

  # Maximum delay for publishing a batch (default: 10ms)
  PULSAR_BATCHING_MAX_PUBLISH_DELAY: "10ms"

  # Compression type (none, zlib, lz4, zstd) (default: none)
  PULSAR_COMPRESSION_TYPE: "none"

  # Other Settings

  # Whether to use one client per slot
  USE_ONE_CLIENT_PER_SLOT: "true"

  # Enable memory usage limitation
  ENABLE_SOFT_MEMORY_LIMIT: "true"

  # Memory limit if ENABLE_SOFT_MEMORY_LIMIT is true
  SOFT_MEMORY_LIMIT: "256mb"

  # Automatically define CPU, memory, message generators and concurrency based on the target event rate set in `MAX_EVENTS_PER_SECOND`
  # Options:
  # - "auto": Automatically calculate resources
  # - "overprovision,X": Overprovision resources by X percent (e.g., "overprovision,20" means overprovision by 20%)
  RESOURCE_CALCULATION: "auto"

  # Timeout when attempting to port forward 
  PORT_FORWARDING_TIMEOUT: "10s"

  # Response validation type (if specified, validates responses from the server)
  # Options:
  # - "user-transformer-hash-email": Validates that email fields are properly hashed with SHA256
  VALIDATOR_TYPE: ""
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

### Running Locally with Docker Compose

For development and testing purposes, you can run load tests locally using Docker Compose. This approach is useful for quick iterations and debugging without deploying to Kubernetes.

#### Prerequisites

- Docker and Docker Compose installed on your machine
- A `docker-compose.yaml` file in the root directory of the project

#### Running a Local Load Test

1. Build the Docker image locally:

```shell
docker build -t rudder-load:local .
```

2. Run the load-runner with the `-local` flag:

```shell
./load-runner -t tests/your-test.yaml -local
```

The load-runner will:
- Create a temporary Docker Compose file with the environment variables from your test configuration
- Start the Docker Compose services
- Run the load test according to the phases defined in your test configuration
- Stop and remove the Docker Compose services when the test completes

#### Environment Variable Overrides

When running locally, the load-runner will override environment variables in the Docker Compose file based on:

1. The `env` section in your test configuration file
2. The `env` section in each phase of your test configuration
3. Any environment variables passed with the `-e` flag

For example, if your test configuration has:

```yaml
name: http
namespace: rudder-load
env:
  MAX_EVENTS_PER_SECOND: 100
  CONCURRENCY: 100
phases:
  - duration: 2m
    replicas: 2
    env:
      MAX_EVENTS_PER_SECOND: 200
```

The load-runner will:
1. Start with `MAX_EVENTS_PER_SECOND=100` and `CONCURRENCY=100`
2. After 2 minutes, update to `MAX_EVENTS_PER_SECOND=200` and `CONCURRENCY=100`

#### Limitations of Local Mode

When running in local mode:
- The `namespace` setting in your test configuration is ignored
- The `reporting` section is not supported
- Resource limits are enforced by Docker rather than Kubernetes
- The test will always use one replica for the load

## Pulsar soak test (`cmd/pulsar-soak-test`)

`cmd/pulsar-soak-test` reconciles an automated 24-hour soak test of a StreamNative Pulsar cluster. A scheduled workflow runs `go run ./cmd/pulsar-soak-test tick` every 15 minutes. Each tick reads its state from a Kubernetes Secret, observes the cluster and the metrics, does whatever is due, and exits. The workflow needs `helm` on the path and a kubeconfig, found with the default loading rules (`$KUBECONFIG`, `~/.kube/config` or in-cluster).

A run goes like this:

- **Detection** — the first tick only records the broker version. A run starts once every broker reports a new version (`pulsar_version_info`), no other version was reported in the last 30 minutes, every broker's image tag carries it, and every broker, bookie and ZooKeeper pod is ready.
- **Announcement** — a top-level Slack message with the old and new versions, the start, the expected end and the workflow run. Every later message is a reply in its thread. `thread_ts=<ts>` is written to `$GITHUB_OUTPUT` when the announcement is posted.
- **Start** — while a rudder-load Deployment from another Helm release is in the namespace, or src-router isn't discarding, the run waits and says so once. Then it runs `helm upgrade --install` with the profile, `namespace`, `deployment.name` (when set), `HTTP_ENDPOINT`, `LOAD_RUN_ID` and `RUN_DEADLINE`. The write keys go in a temporary values file that only its owner can read, deleted once helm is done.
- **During the run** — every tick checks that src-router still discards: the Deployment's env var is `"true"`, its rollout is complete, every pod has it, and no `src_router_http_requests` series in the namespace lacks `discard="true"`. If not, rudder-load is uninstalled at once. If the check can't be done for 30 minutes, the run is aborted too.
- **Stop and hand-off** — at the deadline rudder-load is uninstalled. The brief goes out once every datasource has data from after the stop (at least 2 minutes after it) or 15 minutes have passed, followed by a reply tagging the `@server-eng` user group.
- **A newer version mid-run** — the run is uninstalled and superseded, and the new version gets a new thread and run.
- **Dry run** — with `PULSAR_SOAK_TEST_DRY_RUN=true`, a tick with no run in progress starts one for the current version, with the configured run length. Its messages are marked as a dry run and it doesn't tag `@server-eng`.

Slack messages are posted with `chat.postMessage` only, and never twice: each is recorded as about to be posted before the call and as posted after it, and a message with any record is never posted again. If Slack's answer is lost, later replies go to the channel, naming the run. Everything the tick prints after reading its settings sits inside a `::stop-commands::` block, so text from Slack or Grafana can't issue workflow commands.

### Settings

Every setting is an env var. Secrets are never printed.

- **`PULSAR_SOAK_TEST_NAMESPACE`** — required. The namespace of rudder-load, ingestion and src-router. The state lives there in the Secret `<release>-state`.
- **`PULSAR_SOAK_TEST_HELM_RELEASE`** — default `rudder-load-pulsar-soak-test`. The soak test's own Helm release; any other rudder-load Deployment in the namespace, from another release or applied without Helm, counts as another load test.
- **`PULSAR_SOAK_TEST_HELM_CHART_PATH`** — default `artifacts/helm`.
- **`PULSAR_SOAK_TEST_HELM_PROFILE_PATH`** — default `artifacts/helm/pulsar_soak_test_values.yaml`.
- **`PULSAR_SOAK_TEST_LOAD_DEPLOYMENT_NAME`** — optional. Overrides the profile's `deployment.name`; it must start with `rudder-load-`.
- **`PULSAR_SOAK_TEST_LOAD_HTTP_ENDPOINT`** — required. rudder-load's `HTTP_ENDPOINT`.
- **`PULSAR_SOAK_TEST_LOAD_WRITE_KEYS`** — required, secret. Comma-separated write keys for rudder-load's `SOURCES`.
- **`PULSAR_SOAK_TEST_SRC_ROUTER_DEPLOYMENT`** — required. src-router's Deployment in the namespace.
- **`PULSAR_SOAK_TEST_SRC_ROUTER_DISCARD_ENV`** — default `SRC_ROUTER_GATEWAY_DISCARD_MESSAGES`. The env var that must be `"true"`.
- **`PULSAR_SOAK_TEST_SLACK_CHANNEL`** — required. The channel ID to post in.
- **`PULSAR_SOAK_TEST_SLACK_RUDDER_ASSIST_USER_ID`** — required. Mentioned in the brief.
- **`PULSAR_SOAK_TEST_SLACK_SERVER_ENG_GROUP_ID`** — required. The `@server-eng` user group ID, tagged after the brief.
- **`PULSAR_SOAK_TEST_SLACK_WORKSPACE_URL`** — optional, e.g. `https://example.slack.com`. Used to link a superseded run's thread to the new one; without it the reply has no link.
- **`PULSAR_SOAK_TEST_SLACK_BOT_TOKEN`** — required, secret. A bot token with `chat:write`.
- **`PULSAR_SOAK_TEST_GRAFANA_URL`** — required. Grafana's base URL; queries go through its datasource proxy.
- **`PULSAR_SOAK_TEST_GRAFANA_TOKEN`** — required, secret. A service account token.
- **`PULSAR_SOAK_TEST_GRAFANA_APP_METRICS_UID`** — required. The Mimir datasource with ingestion and src-router metrics.
- **`PULSAR_SOAK_TEST_GRAFANA_KUBE_METRICS_UID`** — required. The Thanos datasource with Kubernetes and Pulsar metrics.
- **`PULSAR_SOAK_TEST_GRAFANA_LOGS_UID`** — required. The Loki datasource.
- **`PULSAR_SOAK_TEST_PULSAR_CLUSTER`** — required. The `cloud_streamnative_io_cluster` label value.
- **`PULSAR_SOAK_TEST_PULSAR_KUBE_NAMESPACE`** — required. The `namespace` label of the Pulsar pods in kube-state-metrics, like `<cluster>:<namespace>`.
- **`PULSAR_SOAK_TEST_BRIEF_TEMPLATE_PATH`** — required. The brief template, with `@RUDDER_ASSIST@`, `@WINDOW_START@` and `@WINDOW_END@`. Every tick fills it with sample times and fails if the result would be over 4,000 bytes, so a broken template stops a run from starting rather than its hand-off.
- **`PULSAR_SOAK_TEST_RUN_LENGTH`** — default `24h`, from `10m` to `72h`.
- **`PULSAR_SOAK_TEST_DRY_RUN`** — default `false`.
- **`GITHUB_SERVER_URL`, `GITHUB_REPOSITORY`, `GITHUB_RUN_ID`** — set by GitHub Actions; together they link the workflow run in the announcement.
- **`GITHUB_OUTPUT`, `RUNNER_TEMP`** — set by GitHub Actions; the thread's ts goes to the first, the temporary values file to the second.

The kubeconfig's identity needs to get, create and update Secrets, get and list Deployments and list Pods in the namespace, plus what `helm` needs to manage the release. To start over, delete the state Secret: the next tick records the current version as its baseline.
