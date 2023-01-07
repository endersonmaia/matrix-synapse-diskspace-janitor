## matrix-synapse-diskspace-janitor

![scruffy the janitor from futurama](frontend/static/images/scruffy.png)

_toilets and boilers, boilers and toilets_



Matrix-synapse (the matrix homeserver implementation) requires a postgres database server to operate. 
It stores a lot of stuff in this postgres database, information about all the rooms that users on the server have joined, etc.

The problem at hand: 

Matrix-synapse stores a lot of data that it has no way of cleaning up or deleting.

Specifically, there is a table it creates in the database called `state_groups_state`:

```
root@matrix:~# sudo -u postgres pg_dump synapse -t state_groups_state --schema-only
--
-- PostgreSQL database dump
--
...

CREATE TABLE public.state_groups_state (
    state_group bigint NOT NULL,
    room_id text NOT NULL,
    type text NOT NULL,
    state_key text NOT NULL,
    event_id text NOT NULL
);
```

I don't understand what this table is for, however, I can recognize fairly easily that it accounts for the grand majority of the disk space bloat of a matrix-synapse instance:

#### top 10 tables by disk space used, cyberia.club instance:

![a pie chart showing state_groups_state using 87% of the disk space](readme/state_groups_state.png)

So, I think it's safe to say that if we can cut down the size of `state_groups_state`, then we can solve our disk space issues.

I know that there are other projects dedicated to this, like https://github.com/matrix-org/rust-synapse-compress-state

However, a cursory examination of the data in `state_groups_state` led me to believe maybe there is an easier and better way.

`state_groups_state` _DOES_ have a `room_id` column on it. It's not _indexed_ by `room_id`, but we can still count the # of rows for each room and rank them:

#### top 100 rooms by number of `state_groups_state` rows, cyberia.club instance:

![a pie chart with two slices taking up about 2 thirds of the pie, and the remaining third taken up mostly by the next 8 slices](readme/top100rooms.png)

In summary, it looks like 

> **about 90% of the disk space used by matrix-synapse is in `state_groups_state`, and about 90% of the rows in `state_groups_state` come from just a handfull of rooms**.

So from this information we have hatched a plan: 

> _Just delete those rooms from our homeserver ![4head](readme/4head.png)_

However, unfortunately the [matrix-synapse delete room API](https://matrix-org.github.io/synapse/latest/admin_api/rooms.html#version-2-new-version) does not remove anything from `state_groups_state`.  

This is similar to the way that the [matrix-synapse message retention policies](https://github.com/matrix-org/synapse/blob/develop/docs/message_retention_policies.md) also do not remove anything from `state_groups_state`.

In fact, probably helps explain why `state_groups_state` gets hundreds of millions of rows and takes up so much disk space: Nothing ever deletes from it!!



