.PHONY: build run test build-pi deploy

BIN := dist/homedash
PI_HOST ?= homedash.local
PI_USER ?= pi

build:
	go build -o $(BIN) ./cmd/homedash

run: build
	./$(BIN)

test:
	go test ./...

build-pi:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o $(BIN)-arm64 ./cmd/homedash

deploy: build-pi
	rsync -av $(BIN)-arm64 $(PI_USER)@$(PI_HOST):/tmp/homedash
	rsync -av deploy/homedash.service $(PI_USER)@$(PI_HOST):/tmp/
	rsync -av deploy/config.example.yaml $(PI_USER)@$(PI_HOST):/tmp/
	ssh $(PI_USER)@$(PI_HOST) 'sudo install -m 0755 /tmp/homedash /usr/local/bin/homedash && sudo install -m 0644 /tmp/homedash.service /etc/systemd/system/homedash.service && sudo systemctl daemon-reload && sudo systemctl restart homedash'
