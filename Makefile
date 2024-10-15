REPLICAS:=1
# How many concurrent go routines can call producer.Publish() at the same time
CONCURRENCY:=100
# NO_OF_TOPICS must be a multiple of 10
NO_OF_TOPICS:=500
TOPIC_PARTITIONS:=2
# KEYS_PER_TOPIC_MAP must have the no. of keys for 10 topics
# The first value will be used for the first 10 topics, the second value for the next 10 topics and so on.
# Then we start again in round-robin fashion.
# e.g. export KEYS_PER_TOPIC_MAP=150_100_50_100_1_20_70_100_5_100
KEYS_PER_TOPIC_MAP:=2_1_5_6_1_2_3_1_5_3
# TRAFFIC_DISTRIBUTION_PERCENTAGE must have 10 elements and the sum of them must be 100
TRAFFIC_DISTRIBUTION_PERCENTAGE:=30_5_5_5_5_30_1_1_17_1
# TOTAL_MESSAGES is the total number of messages to be sent
TOTAL_MESSAGES:=10000 # 10K / 5MB = ~500B per message
# TOTAL_DATA_SIZE_BYTES is the total size of the data to be sent in bytes
# The size of each message is randomized and never greater than MAX_MESSAGE_SIZE_BYTES
# The sum of all the message sizes will be equal to TOTAL_DATA_SIZE_BYTES
TOTAL_DATA_SIZE_BYTES:=5mb # 5MB
# MAX_MESSAGE_SIZE_BYTES is the maximum size of a message
MAX_MESSAGE_SIZE_BYTES:=1kb # 1KB
# TOTAL_DURATION is the total duration of the test
TOTAL_DURATION:=0 # 0 means finish as fast as possible

### KAFKA VARIABLES
KAFKA_WRITE_TIMEOUT:=10s
KAFKA_READ_TIMEOUT:=10s
KAFKA_BATCH_TIMEOUT:=1s
KAFKA_BATCH_SIZE:=100

ifneq (,$(or $(findstring deploy-,$(MAKECMDGOALS)),$(findstring update-,$(MAKECMDGOALS))))
    ifeq ($(DOCKER_USER),)
        $(error DOCKER_USER is not set)
    endif
    ifeq ($(K8S_NAMESPACE),)
        $(error K8S_NAMESPACE is not set)
    endif
endif

ifneq (,$(filter delete logs,$(MAKECMDGOALS)))
    ifeq ($(K8S_NAMESPACE),)
        $(error K8S_NAMESPACE is not set)
    endif
endif

ifeq ($(MAKECMDGOALS),build)
    ifeq ($(DOCKER_USER),)
        $(error DOCKER_USER is not set)
    endif
endif

.PHONY: build
build:
	docker build --progress plain -t $(DOCKER_USER)/rudder-tests-ingestion-producer .
	docker push $(DOCKER_USER)/rudder-tests-ingestion-producer:latest

.PHONY: deploy-%
deploy-%: build
	# Dynamically determine the service name (e.g., "http", "pulsar"...) from the target name
	@$(eval SERVICE_NAME=$*)
	@$(eval VALUES_FILE=$(PWD)/helm/${SERVICE_NAME}_values.yaml)
	@echo Deploying using $(VALUES_FILE)
	helm install rudder-ingester $(PWD)/helm \
		--namespace $(K8S_NAMESPACE) \
		--set namespace=$(K8S_NAMESPACE) \
		--set dockerUser=$(DOCKER_USER) \
		--set deployment.replicas=$(REPLICAS) \
		--values $(VALUES_FILE)

.PHONY: update-%
update-%: build
	# Dynamically determine the service name (e.g., "http", "pulsar"...) from the target name
	@$(eval SERVICE_NAME=$*)
	@$(eval VALUES_FILE=$(PWD)/helm/${SERVICE_NAME}_values.yaml)
	@echo Deploying using $(VALUES_FILE)
	helm upgrade rudder-ingester $(PWD)/helm \
		--namespace $(K8S_NAMESPACE) \
		--set namespace=$(K8S_NAMESPACE) \
		--set dockerUser=$(DOCKER_USER) \
		--set deployment.replicas=$(REPLICAS) \
		--values $(VALUES_FILE)

.PHONY: delete
delete:
	helm uninstall rudder-ingester --namespace $(K8S_NAMESPACE)

.PHONY: logs
logs:
	# TODO
	kubectl logs -f -n $(K8S_NAMESPACE) -l run=rudder-tests-ingestion-producer
