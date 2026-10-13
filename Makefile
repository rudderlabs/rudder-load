ifneq (,$(or $(findstring deploy-,$(MAKECMDGOALS)),$(findstring update-,$(MAKECMDGOALS))))
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

GO=go
GOLANGCI=github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
govulncheck=golang.org/x/vuln/cmd/govulncheck@v1.8.0

.PHONY: lint
lint: ## Run linters on all go files
	$(GO) run $(GOLANGCI) run -v

.PHONY: vulncheck
vulncheck: ## Check for known vulnerabilities reachable from the code
	GOVULNCHECK=$(govulncheck) ./scripts/govulncheck.sh

.PHONY: build
build:
	docker build --progress plain -t $(DOCKER_USER)/rudder-load .
	docker push $(DOCKER_USER)/rudder-load:latest

# Optional: the image to deploy, overriding deployment.image from the values file, e.g.
# IMAGE=422074288268.dkr.ecr.us-east-1.amazonaws.com/rudderstack/rudder-load:0.1.0
IMAGE ?=

.PHONY: deploy-%
deploy-%:
	@$(eval SERVICE_NAME=$*)
	@$(eval VALUES_FILE=$(PWD)/artifacts/helm/${SERVICE_NAME}_values_copy.yaml)
	@echo Deploying using $(VALUES_FILE)
	helm upgrade --install rudder-load-${SERVICE_NAME} $(PWD)/artifacts/helm \
		--namespace $(K8S_NAMESPACE) \
		--set namespace=$(K8S_NAMESPACE) \
		--values $(VALUES_FILE) \
		$(if $(IMAGE),--set deployment.image=$(IMAGE))

.PHONY: delete-%
delete-%:
	@$(eval SERVICE_NAME=$*)
	helm uninstall rudder-load-${SERVICE_NAME} --namespace $(K8S_NAMESPACE)

.PHONY: logs
logs:
	kubectl logs -f -n $(K8S_NAMESPACE) -l run=rudder-load

.PHONY: test
test:
	go test -race -coverprofile=coverage.txt -covermode=atomic ./...;
