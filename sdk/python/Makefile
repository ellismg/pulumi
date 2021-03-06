PROJECT_NAME     := Pulumi Python SDK
LANGHOST_PKG     := github.com/pulumi/pulumi/sdk/python/cmd/pulumi-language-python
VERSION          := $(shell ../../scripts/get-py-version)

ifeq ($(PYTHON),)
	PYTHON := python
endif

ifeq ($(PIP),)
	PIP := pip
endif

ifneq ($(PYTHON_3),)
	PIPENV_PYTHON_VERSION := --three
else
	PIPENV_PYTHON_VERSION := --two
endif

PIPENV_VERSION := 2018.05.18
PYENV := ./env
PYENVSRC := $(PYENV)/src

GOMETALINTERBIN := gometalinter
GOMETALINTER    := ${GOMETALINTERBIN} --config=../../Gometalinter.json

include ../../build/common.mk

ensure::
	$(PIP) install pipenv==$(PIPENV_VERSION) --user
	pipenv $(PIPENV_PYTHON_VERSION) install --dev
	mkdir -p $(PYENVSRC)

build::
	rm -rf $(PYENVSRC) && cp -R ./lib/. $(PYENVSRC)/
	sed -i.bak "s/\$${VERSION}/$(VERSION)/g" $(PYENVSRC)/setup.py && rm $(PYENVSRC)/setup.py.bak
	if [ ! -z "$$(command -v pandoc)" ]; then \
		pandoc --from=markdown --to=rst --output="$(PYENVSRC)/README.rst" ../../README.md; \
	else \
		echo "warning: pandoc not found, generating empty README.rst"; \
		echo "" > "$(PYENVSRC)/README.rst"; \
	fi
	cd $(PYENVSRC) && pipenv run python setup.py build bdist_wheel --universal
	go install -ldflags "-X github.com/pulumi/pulumi/sdk/python/pkg/version.Version=${VERSION}" ${LANGHOST_PKG}

lint::
	pipenv run pylint -E ./lib/pulumi --ignore-patterns '.*_pb2_.*.py'
	$(GOMETALINTER) ./cmd/pulumi-language-python/... | sort ; exit $${PIPESTATUS[0]}
	$(GOMETALINTER) ./pkg/... | sort ; exit $${PIPESTATUS[0]}

install::
	cd $(PYENVSRC) && $(PIP) install --user -e .
	cp ./cmd/pulumi-language-python-exec "$(PULUMI_BIN)"
	GOBIN=$(PULUMI_BIN) go install \
		  -ldflags "-X github.com/pulumi/pulumi/sdk/python/pkg/version.Version=${VERSION}" ${LANGHOST_PKG}

test_fast::
	pipenv run pip install ./env/src
	pipenv run python -m unittest discover -s lib/test

dist::
	go install -ldflags "-X github.com/pulumi/pulumi/sdk/python/pkg/version.Version=${VERSION}" ${LANGHOST_PKG}
	cp ./cmd/pulumi-language-python-exec "$$(go env GOPATH)"/bin/
