# AutoDelete

### _cleaning up over 12000 channels since 2017_

**AutoDelete** is a Discord bot that will automatically delete messages from a designated channel.

Messages are deleted on a "rolling" basis -- if you set a 24-hour live time, each message will be deleted 24 hours after it is posted (as opposed to all messages being deleted every 24 hours).

If you have an urgent message about the operation of the bot, say `@AutoDelete adminhelp ... your message here ...` and I'll get back to you as soon as I see it.

Add it to your server here: https://autodelete.riking.org/discord_auto_delete/oauth/start

**[Support me on Patreon](https://patreon.com/riking)** if you enjoy the bot or to help keep it running! https://www.patreon.com/riking

Announcements server: https://discord.gg/FUGn8yE

## Usage

Create a new "purged" channel where messages will automatically be deleted. Someone with MANAGE_MESSAGES permission (usually an admin) needs to say `@AutoDelete start 100 24h` to start the bot and tell it which channel you are using.

The `100` in the start command is the maximum number of live messages in the channel before the oldest is deleted.
The `24h` is a duration after which every message will be deleted. [Acceptable units](https://godoc.org/time#ParseDuration) are `h` for hours, `m` for minutes, `s` for seconds. *Warning*: Durations of a day or longer still need to be specified in hours.

Make sure to mention the **bot user** and not the role alias!

![Select the mention option with #6949 on the end.](docs/mention-user-not-role.png)

A "voice-text" channel might want a shorter duration, e.g. 30m or 10m, when you just want "immediate" chat with no memory.

*The bot must have permission to read (obviously) and send messages in the channel you are using*, in addition to the Manage Messages permission. If the bot is missing permissions, it will disable itself and attempt to tell you, though this usually won't work when it can't send messages.

To turn off the bot, use `@AutoDelete set 0` to turn off auto-deletion.

For a quick reminder of these rules, just say `@AutoDelete help`.

If you need extra help, say `@AutoDelete adminhelp ... message ...` to send a message to the support guild.

## Policy

_The following section is a DRAFT and may be incomplete and is subject to change, though the information present is correct to the best of my knowledge._

No message content is ever retained, except in the case when a message "@-mentions" the bot, where it may be retained to provide support or improve the bot. The "adminhelp" command transmits the provided message content to a channel in Discord and is subject to Discord's retention policies. Deleting a command invocation via the Discord interface has no effect on how long the bot's information about the invocation is stored.

The "community instance" of the bot will retain operational usage data, including data that identifies a particular guild or channel ID and/or with high-resolution timestamps. The full form of this data will be retained for between 15 and [TBD: research Prometheus collector guarantees] days, and aggregated or summarized forms will be retained for up to 1.5 years. Usage data will not be used for commercial purposes except for the purpose of encouraging people to financially support the bot in a non-automated manner (in particular, usage data will not be sold or provided to any third party).

Contact Riking via the announcements server if you would like to request a copy of this data under the GDPR or equivalent consumer rights legislation.

The settings for a channel are kept on disk with the channel ID, guild ID, pinned message IDs, pin version timestamp, and time / count settings together. In the case that a channel is removed from the bot, either through `set 0` or kicking the bot from the server, these settings are deleted. Backup or archival copies of the settings may be retained indefinitely but will not be used except for the purposes of disaster recovery.

Any changes to this policy will be announced on the support server in the #announce channel.
