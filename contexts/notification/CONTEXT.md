# Notification

The context that tells people what happened on the platform. It records every Notification it
sends and delivers each one over one or more Channels at once.

## Language

**Notification**:
One message the platform sends a person about something that happened, such as a Droplet becoming
active or a payment failing. Always recorded, whichever Channels carry it.
_Avoid_: alert, message, event (an event is what a Notification is about, not the Notification)

**Notification type**:
What a Notification is about, such as `droplet_active` or `payment_failed`. It fixes which Channels
carry the Notification and, for one about a Team, which Permission a member needs to receive it.
_Avoid_: kind, template, category

**Recipient**:
The person a Notification is for: a User, or only an email address when the person is not a User
(someone invited before they sign up, the invoice address on a billing profile). Only a User has
an Inbox.
_Avoid_: receiver, target, addressee

**Channel**:
A way a Notification reaches a person: `email`, `sms` or `in_app`. One Notification can go out
over several Channels at the same time.
_Avoid_: transport, medium, provider (a provider is what sits behind a Channel)

**In-app**:
The Channel that puts a Notification in a User's Inbox inside the web app. It still works when the
User is not online: the Inbox shows the Notification when they come back.
_Avoid_: socket, push, realtime (those describe how it arrives while the User is online, not the
Channel)

**Inbox**:
The In-app Notifications of one User, each read or unread for that User alone.
_Avoid_: feed, notification center, bell (the bell is the web app's control for it)
