build:
	go build

test:
	go test ./...

integration: build
	go test -tags integration -run TestIntegration -v -timeout 20m .

install: build
	cp autopush $(HOME)/.bin/autopush
	chmod +x $(HOME)/.bin/autopush
