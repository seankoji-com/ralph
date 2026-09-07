.PHONY: build run demo test smoke check

build:
	go build -o bin/ralph .

run: build
	./bin/ralph

demo: build
	./bin/ralph --demo

test:
	go test -race ./...

smoke: build
	python3 scripts/smoke.py

check: test
	go vet ./...
