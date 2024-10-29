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

.PHONY: build
build:
	docker build --progress plain -t $(DOCKER_USER)/rudder-load .
	docker push $(DOCKER_USER)/rudder-load:latest

.PHONY: deploy-%
deploy-%:
	@$(eval SERVICE_NAME=$*)
	@$(eval VALUES_FILE=$(PWD)/artifacts/helm/${SERVICE_NAME}_values_copy.yaml)
	@echo Deploying using $(VALUES_FILE)
	helm install rudder-load-${SERVICE_NAME} $(PWD)/artifacts/helm \
		--namespace $(K8S_NAMESPACE) \
		--set namespace=$(K8S_NAMESPACE) \
		--values $(VALUES_FILE)

.PHONY: delete-%
delete-%:
	@$(eval SERVICE_NAME=$*)
	helm uninstall rudder-load-${SERVICE_NAME} --namespace $(K8S_NAMESPACE)

.PHONY: logs
logs:
	kubectl logs -f -n $(K8S_NAMESPACE) -l run=rudder-load
