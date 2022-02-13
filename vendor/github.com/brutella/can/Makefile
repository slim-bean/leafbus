GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get

VERSION=$(shell git describe --exact-match --tags 2>/dev/null)
BUILD_DIR=build
PACKAGE_RPI=candump-$(VERSION)_linux_armhf

# unset GOPATH to us Go modules
unexport GOPATH

test:
	$(GOTEST) -v ./...

clean:
	$(GOCLEAN)
	rm -rf $(BUILD_DIR)

package-rpi: build-rpi
	tar -cvzf $(PACKAGE_RPI).tar.gz -C $(BUILD_DIR) $(PACKAGE_RPI)

build-rpi:
	GOOS=linux GOARCH=arm GOARM=6 $(GOBUILD) -o $(BUILD_DIR)/$(PACKAGE_RPI)/usr/bin/candump -i cmd/candump.go