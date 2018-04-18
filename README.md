# AutoDelete

AutoDelete is a Discord bot that will automatically delete messages from a designated channel.

Add it to your server here: https://home.riking.org/discord_auto_delete/oauth/start

## Usage

Create a new "purged" channel where messages will automatically be deleted. Someone with MANAGE_MESSAGES permission (usually an admin) needs to say `@AutoDelete start 100 24h` to start the bot and tell it which channel you are using.

The `100` in the start command is the maximum number of live messages in the channel before the oldest is deleted.
The `24h` is a duration after which every message will be deleted. [Acceptable units](https://godoc.org/time#ParseDuration) are `h` for hours, `m` for minutes, `s` for seconds. Durations of a day or longer still need to be specified in hours.

A "voice-text" channel might want a shorter duration, e.g. 1h or 30m, when you just want "immediate" chat with no computer memory.
