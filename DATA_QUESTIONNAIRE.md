## Application Details
_What does your application do? Please be as detailed as possible, and feel free to include links to image or video examples._

**AutoDelete** is a Discord bot that will automatically delete messages from a designated channel.

After adding the bot to a server, users with the 'Manage Messages' permission can interact with and configure the bot. The bot is configured by chat commands, consisting of an @-mention of the bot followed by the command. Configuration is per-channel, not per-guild.

The configuration calculates an expiration condition for every message sent in the channel, either through time or through a message count limit being reached. When the oldest message in a channel expires, it and any other messages expiring in the next few seconds are bulk-deleted from Discord.

The end. That's all it does :-)
The rest is just details!

## Data Collection
_Tell us more about the data you store and process from Discord._

### What Discord data do you store?

The text of all messages **@-mentioning** the bot are retained for a limited time for support and product improvement purposes.

The text of all messages using the `adminhelp` command are forwarded to another Discord channel for human inspection. Discord storage policies then take over.

The bot-specific configuration that a user sets is retained **indefinitely**, in connection with the channel and guild ID the configuration is for. This data also carries ext4 file modification timestamps.

Role membership in the support guild is interrogated to determine if a user is a donor to the bot via Patreon. The results of this query (yes or no) are saved to the configuration file.

No other message content is ever retained. Messages in channels configured for automatic deletion are stripped down to just their ID and timestamp after being processed.

The list of "live" message IDs and precise timestamps in a channel that will be deleted by the bot at their expiration date are retained **in memory only** for the duration that those messages remain undeleted on Discord.

The list of pinned messages in a channel configured for automatic deletion is requested by the bot and maintained in memory for as long as the channel remains configured for automatic deletion.

Channel names, IDs, and message counts (if applicable) are printed to the log when the bot operates on them. These logs are kept in a rotating buffer in RAM using `tmux` and overwritten frequently, so it's hard to nail down an exact duration - but I'm willing to commit in writing that these logs are retained no longer than 15 days.
Bot mentions are printed to that same log.

Aggregated statistics are exported to a Prometheus monitoring server. Disaggregated statistics that identify particular guild and channel IDs are exported to a Prometheus monitoring server when, and only when, per-channel usage thresholds are exceeded. (This protects both the privacy of low-volume users, as well as my RAM.) All Prometheus statistics are collected at an effective time granularity no less than 1 minute apart (it's 5 minutes).

### For what purpose(s) do you store it?

See earlier answers for detailed descriptions of the data categories.

The following purposes are used below:

 - Essential means purposes that cannot be removed from the operation of the bot without significantly compromising its functionality.
 - Operational means purposes connected to making sure the bot keeps running.
 - Debugging means investigating and responding to problems in the operations of the bot.
 - Support means responding to queries from bot users about usage or issues they encounter.
 - Product improvements means improving the bot's features, operations, stability, or similar.
 - Usage analysis means checking for excessive or pathological use of the bot, reaching out to particular users of the bot, or producing non-identifying statistics for marketing purposes (e.g. the line "cleaning up over 8000 channels since 2017" present in README.md).

Message IDs, etc: Essential
Log lines: Operational and debugging
Structured logging: Product improvements, usage analysis
@-mention content: Support
Metrics: Operational, debugging, product improvements, usage analysis, support
Configuration: Essential, usage analysis
Patreon integration query results: Same as Configuration
adminhelp content: Support

### For how long do you store it?

See earlier answers for detailed descriptions.

 - Volatile: Only ever stored in RAM. Fetched from the canonical data source on every process restart.
 - Ephermal: Only ever stored in RAM. Outlives a bot process but not a machine reboot.
 - Rotated: Written to disk or tmpfiles and cleaned up on a regular basis.
 - Durable: Written to disk and expected to survive a machine reboot. Subject to disaster recovery plans.

Message IDs, etc: Volatile, process lifetime
Log lines: Ephermal, <15 days
Structured logging: Rotated, <15 days [Aspirational. Current: Volatile]
@-mention content: Rotated, <15 days [Aspirational. Current: 'Log lines']
Metrics: Durable, <18 months
Configuration: Durable, indefinite
Patreon integration query results: Same as Configuration
adminhelp content: Outsourced to Discord

### What is the process for users to request deletion of their data?

Feels a bit odd, as deleting data is the entire purpose of the bot!

A server administrator with 'Manage Messages' can delete the configuration for a channel by using the `@AutoDelete set 0` invocation.

A server administrator with 'Manage Server' can delete all configuration data for a guild by kicking the bot. After an indeterminate amount of time (hint: it's the next websocket gateway reconnect), the bot will notice it no longer has access to the channel, and automatically delete the configuration data.

No process is implemented for deletion of log data, as this data is retained for less than 30 days.

For deletion of 'adminhelp' command content, users can make a request through the same channels as listed in the 'security issues' section.

## Infrastructure
_Tell us more about your application's infrastructure and your team's security practices._

### What systems and infrastructure do you use?

The bot runs on one (or more) droplets on DigitalOcean.

_(looks at recommended shard count again)_ Yup, the multi-droplet split is coming up real soon now.

### How have you secured access to your systems and infrastructure?

Access to the droplet is only possible through key-authenticated SSH. SSH private key files are protected using passphrases of at least 15 characters in length, or other mechanisms derived in the future that are at least as secure.

Code deploys are performed over SSH and always pull the integrity-checked code from Github, no manual uploads of binaries or source to the server are performed.

_(quickly goes to delete the localhost:8000 OAuth redirect url. I have a test bot instance for that stuff now)_

The OAuth flow to add the bot to a guild happens exclusively via HTTPS at https://autodelete.riking.org/ or https://home.riking.org/ . DNS for the domain is managed by CloudFlare. SSL certificates are issued by Let's Encrypt.

### How can users contact you with security issues?

 1. Users can join the bot information and announcements guild on Discord at https://discord.gg/FUGn8yE and ask non-sensitive questions or make non-sensitive requests there.
 2. Users can join the above mentioned guild and private message me, @Riking#6902.
 3. Users who do not wish to use the above methods can contact me via email at <rikingcoding@gmail.com> by mentioning "AutoDelete" in the subject line of the email.

### Does your application utilize other third-party auth services or connections? If so, which, and why?

The Patreon integration is used exclusively via querying the Discord side of the integration.

No third-party services are actively contacted during bot operations, and no other third-party services are used.

## Privileged Gateway Intents
_Maintaining a stateful application can be difficult when it comes to the amount of data you're expected to process, especially at scale. Gateway Intents are a system to help you lower that computational burden. Some of these gateway intents are defined as "Privileged" due to the sensitive nature of the data they grant, and access can be enabled below._

Which intents are you applying for, if any? (Leave blank if you do not need any of these)

 - [ ] Presence Intent
 - [ ] Server Members Intent
