# AutoDelete

AutoDelete is a Discord bot that will automatically delete messages from a designated channel.

Messages are deleted on a "rolling" basis -- if you set a 24-hour live time, each message will be deleted 24 hours after it is posted (as opposed to all messages being deleted every 24 hours).

Add it to your server here: https://home.riking.org/discord_auto_delete/oauth/start

Announcements server: https://discord.gg/FUGn8yE

## Usage

Create a new "purged" channel where messages will automatically be deleted. Someone with MANAGE_MESSAGES permission (usually an admin) needs to say `@AutoDelete start 100 24h` to start the bot and tell it which channel you are using.

The `100` in the start command is the maximum number of live messages in the channel before the oldest is deleted.
The `24h` is a duration after which every message will be deleted. [Acceptable units](https://godoc.org/time#ParseDuration) are `h` for hours, `m` for minutes, `s` for seconds. *Warning*: Durations of a day or longer still need to be specified in hours.

A "voice-text" channel might want a shorter duration, e.g. 30m or 10m, when you just want "immediate" chat with no memory.

To turn off the bot, use `@AutoDelete set 0` to turn off auto-deletion.

For a quick reminder of these rules, just say `@AutoDelete help`.
