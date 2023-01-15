# Hiatus, Unsupported Versions, Rate Limiting & Self Hosting

The creator of this bot is on an extended break, with no ETA to return to this project.

The below instructions are kept as a guide for existing installs. The share community version of the bot is rate limited. It sometimes works and sometimes doesn't.

**Using the shared community version of this bot is no longer supported and not recommended.**

Self-Hosting the bot (via Azure, AWS, Oracle Cloud, Docker instances) is provided via the Discord as a best effort process, but the underlying code is no longer being actively maintained. This message will be removed when this is no longer the case

-- 15-JAN-2023

# AutoDelete

### _retention policies for 'gamers'_

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

## Deployment

### Custom

See the [docs](./docs) directory for setup scripts and the configuration files that run the official bot instance.

### Docker

How to build the docker containers:

```
docker build -t myimages/autodelete:tag .
```

Pre-built docker containers have been uploaded by the community to https://hub.docker.com/ if you wish to use them. These image owners should also be contactable over the support Discord server.

Required Mounts: 

```
/path/to/storage/config.yml:/autodelete/config.yml
/path/to/storage/data/:/autodelete/data/
```

Example:

```
docker run -d -p 2202:2202/tcp \
 --name Autodelete \
 -v /opt/AutoDelete/config.yml:/autodelete/config.yml \
 -v /opt/AutoDelete/data/:/autodelete/data/ \
 --restart=always \
 myimages/autodelete:tag
```

## Policy

The following two sections apply only to the hosted, community instance that can be invited to your server at the link above, as well as the help server and this GitHub repository.

Any changes to the following policies will be announced on the support server in the #announce channel.

### Privacy

_The following section is a DRAFT and may be incomplete and is subject to change, though the information present is correct to the best of my knowledge._

No message content is ever retained, except in the case when a message "@-mentions" the bot, where it may be retained to provide support or improve the bot. The "adminhelp" command transmits the provided message content to a channel in Discord and is subject to Discord's retention policies. Deleting a command invocation via the Discord interface has no effect on how long the bot's information about the invocation is stored.

The "community instance" of the bot will retain operational usage data, including data that identifies a particular guild or channel ID and/or with high-resolution timestamps. The full form of this data will be retained for 45 days ([cite](docs/prometheus-autodelete-aggregator.service#L6)), and aggregated or summarized forms will be retained for up to 1.5 years. Usage data will not be used for commercial purposes except for the purpose of encouraging people to financially support the bot in a non-automated manner (in particular, usage data will not be sold or provided to any third party).

In order to faciliate product support, and response and detection of violations of the Acceptable Use Policy, an automated scan of your Guild structure will be performed and a report produced, with a focus on users and roles carrying the _Manage Messages_ permission and channels where the bot is or was active. These reports may be shared with a limited audience to the extent necessary to identify or cure violations of the Acceptable Use Policy.

Contact Riking via the announcements server if you would like to request a copy of this data under the GDPR or equivalent consumer rights legislation.

The settings for a channel are kept on disk with the channel ID, guild ID, pinned message IDs, pin version timestamp, and time / count settings together. In the case that a channel is removed from the bot, either through `set 0` or kicking the bot from the server, these settings are deleted. Backup or archival copies of the settings may be retained indefinitely but will not be used except for the purposes of disaster recovery.

### Acceptable Use

The bot may not be used to perform or to assist with any of the following actions:

 - improperly use support channels to make false reports;
 - engage in conduct that is fraudulent or illegal;
 - generally, to cover up, hide, or perform any violation of the Discord Terms of Service;
 - to cause the bot to violate the Discord Terms of Service if it would not have violated those terms without your actions;
 - strain the technical infrastructure of the bot with an unreasonable volume or rate of requests, or requests designed to create an unreasonable load, except with explicit permission to conduct a specific technical or security test;
 - any use of the bot in a role where malfunction, improper operation, or normal operation of the bot could cause damages that exceed the greater of (a) $100 US Dollars or (b) the amount you have paid to the Operator of the bot;

Violations of the Acceptable Use Policy may be dealt with in one or more of the following manners:

 - An informal warning from the Operator, sent via the help server, via Discord DM, or from the bot's account through Discord.
 - A formal warning from the Operator, sent from the bot's account through Discord.
 - Removal of service from your guild, with or without warning.
 - Refusal of service to any guilds a particular user operates or has moderation capabilities on.
 - Referral of incident details to Discord, law enforcement, or other competent authorities.
 - Cooperating with investigations by Discord, law enforcement, or other competent authorities.

While the above list of remedies is generally ordered by severity, the Operator has no obligation to respect the ordering of the list, to enact any specific remedy, or to take action against any specific violation. Lack of action in response to a violation is not a waiver against future remedial action (in particular, note limited investigational capacity).

If you cannot comply with the Acceptable Use Policy, you must download the code of the bot and run it on your own infrastructure, accepting all responsibility for your actions.

### Limitation of Liability

***Under no circumstance will Operators's total liability arising from your use of the service exceed the greater of (a) the amount of fees Operator received from you or (b) $100 US dollars. This includes consequential, incidential, special, punitive, or indirect damages, based on any legal theory, even if the side liable is advised that the other may suffer damages, and even if You paid no fees at all.*** Some jurisdictions do not allow the exclusion of implied warranties or limitation of liability for incidental or consequential damages. In these jurisdictions, Operator's liability will be limited to the greatest extent permitted by law.

The service is provided to you without obligation of payment, and it is your responsibility to take actions to account for potentially harmful actions it may perform.

As a reminder, the Apache License, Version 2, and not the above paragraphs, applies to source distributions of this software:

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
