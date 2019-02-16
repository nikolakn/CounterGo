#

NAME = Counter
PACKAGE = github.com/ChizhovVadim/CounterGo
VERSION = v3.2

all:
	GOOS=linux   GOARCH=amd64 go build -o $(NAME)-$(VERSION)-linux-64       $(PACKAGE)
	GOOS=windows GOARCH=amd64 go build -o $(NAME)-$(VERSION)-windows-64.exe $(PACKAGE)
	GOOS=darwin  GOARCH=amd64 go build -o $(NAME)-$(VERSION)-osx-64         $(PACKAGE)
