# AutoDelete Discord bot

Docker Container for self hosting Rikings' AutoDelete discord bot. For more information about the discord bot please go to Rikings' github:  https://github.com/riking/AutoDelete 

This discord bot does rolling deletes of messages. The deletion is based off of how many messages you would like to keep in a given window, or how long you would like to keep the messages. This docker container is not supported by Riking, but rather supported by jacoknapp. If you find an issue that is container related (rather than application related) please drop an issue into the github repository. and I will do my best to resolve it. 

## Tag Information

latest - This will be the most update to date working build.  
test - This is an automated amd64 build. It will contain the latest version of golang, and my github test branch. May not always work.    
version tags - These are snapshot builds, they are generically bug free and will never change. 

## Required Mounts


### /path/to/storage/config.yml:/autodelete/config.yml

Download the example config [here](https://raw.githubusercontent.com/riking/AutoDelete/master/config.example.yml). 

This contains the configuration file required to run the bot. See the github for more information about what should go into the config file. You will need to create a developer discord bot in the discord portal located: [here](http://discordapp.com/developers/applications/me). The bot requires read, send and manage message permissions in order to work.

```
clientid: "****************"  //client id that you get from the discord bot site
clientsecret: "****************" //client secret that you get from the discord bot site
bottoken: "*****************************" // bot token from the creation of the discord bot site
adminuser: "*************" // the user id for the admin. Able to be gotten by setting your discord to development mode, and right clicking on the user.
http:
  listen: "0.0.0.0:2202"
  public: "http://0.0.0.0" // this can be both an ip address or url and doesn't have to be set to be available externally
backlog_limit: 200
errorlog: "/autodelete/data/error.log" 
statusmessage: "in the garbage" // you can change this to be the status of your discord bot
```


### /path/to/storage/data/:/autodelete/data/

This storage location is required to keep the channel, message and other persistent information. 


## Docker Run Example

```
docker run -d -p 2202:2202/tcp \
 --name Autodelete \
 -v /opt/AutoDelete/config.yml:/autodelete/config.yml \
 -v /opt/AutoDelete/data/:/autodelete/data/ \
 --restart=always \
 jacoknapp/autodelete-discord:latest
```

## Bot Setup

In order to add this to your server you are going to have to go to :
http://ip-address:2202/discord_auto_delete/oauth/start

(note: Ip-address is the ip address of your docker server, or the http url you can access your docker server on)


Once added to your server you have to enable it on each channel you would like the bot to run on. The following commands are available to enable the bot:

```
@AutoDelete set 0 0h // this turns off the autodelete bot for the channel
@AutoDelete set 6 // This will only keep six messages in the channel. The six can be replaced with any number.
@AutoDelete set 24h // This will keep the last 24 hours worth of messages. This number can be in increments of hours (h), minutes(m) or seconds(s) or a combination of the 3
@AutoDelete help // get more information on how the bot runs
```

Pinned messages will not be automatically deleted, provided that the message is pinned about 10 minutes after Autodelete is installed on a channel.

For more information about how the autodelete bot works please reference Rikings github readme: https://github.com/riking/AutoDelete

## Legal

Copyright 2020 Jacob Knapp

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
