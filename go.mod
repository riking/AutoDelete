module github.com/riking/AutoDelete

go 1.13

require (
	github.com/bwmarrin/discordgo v0.17.0
	github.com/golang/protobuf v1.3.1
	github.com/gorilla/websocket v1.4.0
	github.com/pkg/errors v0.8.1
	golang.org/x/crypto v0.0.0-20190411191339-88737f569e3a
	golang.org/x/net v0.0.0-20190404232315-eb5bcb51f2a3
	golang.org/x/oauth2 v0.0.0-20190402181905-9f3314589c9a
	golang.org/x/sys v0.0.0-20190412213103-97732733099d
	google.golang.org/appengine v1.5.0
	gopkg.in/yaml.v2 v2.2.3-0.20190319135612-7b8349ac747c
)

replace github.com/bwmarrin/discordgo => ./vendor/github.com/bwmarrin/discordgo
