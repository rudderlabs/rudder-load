REPLICAS:=2
# Make sure to escape commas in the SOURCES variable like so: writeKey1\,writeKey2
SOURCES:=2m8pTOW8tHPZkjMXI0neUq9CGMt\,2m8pWqHVEudTqKIaGPDHBE0rzCz

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
	docker build --progress plain -t $(DOCKER_USER)/rudder-ingester .
	docker push $(DOCKER_USER)/rudder-ingester:latest

.PHONY: deploy-%
deploy-%: build
	# Dynamically determine the service name (e.g., "http", "pulsar"...) from the target name
	@$(eval SERVICE_NAME=$*)
	@$(eval VALUES_FILE=$(PWD)/artifacts/helm/${SERVICE_NAME}_values.yaml)
	@echo Deploying using $(VALUES_FILE)
	helm install rudder-ingester $(PWD)/artifacts/helm \
		--namespace $(K8S_NAMESPACE) \
		--set namespace=$(K8S_NAMESPACE) \
		--set dockerUser=$(DOCKER_USER) \
		--set deployment.replicas=$(REPLICAS) \
		--set deployment.env.SOURCES="$(SOURCES)" \
		--set deployment.env.HTTP_ENDPOINT="http://$(K8S_NAMESPACE)-ingestion.$(K8S_NAMESPACE):8080/v1/batch" \
		--values $(VALUES_FILE)

.PHONY: update-%
update-%: build
	# Dynamically determine the service name (e.g., "http", "pulsar"...) from the target name
	@$(eval SERVICE_NAME=$*)
	@$(eval VALUES_FILE=$(PWD)/artifacts/helm/${SERVICE_NAME}_values.yaml)
	@echo Deploying using $(VALUES_FILE)
	helm upgrade rudder-ingester $(PWD)/artifacts/helm \
		--namespace $(K8S_NAMESPACE) \
		--set namespace=$(K8S_NAMESPACE) \
		--set dockerUser=$(DOCKER_USER) \
		--set deployment.replicas=$(REPLICAS) \
		--set deployment.env.SOURCES="$(SOURCES)" \
		--set deployment.env.HTTP_ENDPOINT="http://$(K8S_NAMESPACE)-ingestion.$(K8S_NAMESPACE):8080/v1/batch" \
		--values $(VALUES_FILE)

.PHONY: delete
delete:
	helm uninstall rudder-ingester --namespace $(K8S_NAMESPACE)

.PHONY: logs
logs:
	kubectl logs -f -n $(K8S_NAMESPACE) -l run=rudder-ingester
