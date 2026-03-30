build:
	go build

install: build
	cp autopush $(HOME)/.bin/autopush
	chmod +x $(HOME)/.bin/autopush
