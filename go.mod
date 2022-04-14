module github.com/riking/AutoDelete

go 1.13

require (
	github.com/bwmarrin/discordgo v0.24.0
	github.com/dgryski/go-sip13 v0.0.0-20200911182023-62edffca9245
	github.com/gorilla/websocket v1.5.0 // indirect
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.12.1
	github.com/prometheus/client_model v0.2.0
	github.com/prometheus/common v0.33.0
	golang.org/x/crypto v0.0.0-20220411220226-7b82a4e95df4 // indirect
	golang.org/x/net v0.0.0-20220412020605-290c469a71a5 // indirect
	golang.org/x/oauth2 v0.0.0-20220411215720-9780585627b5
	golang.org/x/sys v0.0.0-20220412211240-33da011f77ad // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/protobuf v1.28.0 // indirect
	gopkg.in/yaml.v2 v2.4.0
)

replace github.com/bwmarrin/discordgo => ./vendor/github.com/bwmarrin/discordgo
