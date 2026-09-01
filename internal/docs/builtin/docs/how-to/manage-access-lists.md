# Manage Access Lists

> **Retired.** Access groups and grants replace this mechanism — see
> [Manage Access](manage-access.md). The admin page described below no longer
> exists and its address redirects. Your configuration was translated
> automatically on upgrade; nothing here needs redoing by hand. This page is
> kept as a record of how the old model worked.

An **access list** is a named set of users and groups that projects can share. Give several projects the same list and they all admit exactly the same people; change the list once and every one of them follows.

Use an access list when the same audience keeps recurring — a team, a customer, a department. Use **custom** visibility instead when a project's audience is genuinely its own, and **private** when the audience is "everyone on the global access list".

## Create a List

1. Log in as an admin
2. Go to **Admin > Access Lists** (or navigate to `/admin/access-lists`)
3. Enter a **Name** — this is what you will pick in the project's visibility dropdown — and an optional description
4. Click **Create List**

A new list admits nobody until you add members to it.

## Add Members

Each member names one subject and the role it confers:

- **User** — a username. Applies immediately, including to users created later.
- **LDAP Group** — a group DN. Reaches each user at their next sign-in.
- **OAuth2 Group** — a group name. Reaches each user at their next sign-in.

Roles are `viewer` (read-only) or `editor` (read and upload). A list can mix all three kinds freely — an LDAP group for the team plus two named contractors is a perfectly ordinary list. Where several members cover the same person, the strongest role wins.

Group membership is only known while a user signs in, which is why group members take effect at their next login while named users apply straight away.

## Point a Project at a List

1. Go to **Admin > Projects**, then **Edit** on the project
2. Set **Visibility** to **Access list**
3. Choose the list under **Access List**
4. Save

The project is now visible to exactly that list's members. Per-project grants still work on top, so you can let one extra person in without touching the list.

## Remove a List

A list that still governs a project cannot be deleted — the admin page will tell you how many projects are in the way. Change those projects' visibility first, then delete the list. Removing a member takes effect immediately for named users, and at the next sign-in for group members.

## How It Differs From Global Access

| | Global access | Access list |
|---|---|---|
| Applies to | All `private` projects at once | Only projects that point at the list |
| How many | One list per instance | As many as you like |
| Configured in | `config.yaml` or **Admin > Global Access** | **Admin > Access Lists** |

See [Manage Global Access](manage-global-access.md) and [Roles and Permissions](../reference/roles-permissions.md).
