# Manage Access

Access is described with two things: **access groups** say who people are, and **grants** say what a group or person may do, and where.

That is the whole model. There is no separate mechanism for LDAP, for instance-wide access, or for reusable lists — those were four different answers to the same question, and they are now one.

## The One Rule

> Your role on a project is the strongest role any grant gives you, on the project or on its organization.

Roles are always the same three, whatever the scope:

| Role | Can |
|---|---|
| **Viewer** | Read the documentation |
| **Editor** | Read, and upload or delete versions |
| **Admin** | Read, upload, and manage the project's settings and access |

## Access Groups

**Admin > Access Groups.** A group is a named set of people, and a member is one of three things:

| Member type | Resolves |
|---|---|
| **User** | Immediately, by username |
| **LDAP group** | At each member's next sign-in |
| **OAuth2 group** | At each member's next sign-in |

Mix them freely: an LDAP group plus two named contractors is an ordinary group.

Group membership carries **no role**. That is deliberate — it is what lets one group be an editor on one project and a viewer on another. The role belongs to the grant.

A member naming an LDAP or OAuth2 group only takes effect once that person signs in, because group membership is known while they authenticate and is not stored anywhere else. Naming a **user** needs no sign-in and applies at once.

Both this page and **Admin > Organizations** list one collapsed card per entry — its name, how many projects or grants it holds, and its description. Click **Edit** on a card to unfold its fields, members and grants; click **Close** to fold it away again. Above a list of more than one, a box filters it by name or description.

## Granting Access

On **Admin > Projects > Edit**, or on an organization under **Admin > Organizations** (open its card with **Edit**), the Access table lists who can reach it. Grant to either:

- an **access group** — the usual case, and the one that scales;
- a **user** — for the one-off, so you never have to invent a group of one.

Both take a role. Granting the same subject twice changes its role rather than adding a second row.

## Organizations

Every project belongs to exactly one organization. A role granted on an organization applies to **every project in it** — that is what makes an organization a boundary rather than a label.

Use it for the access that would otherwise be repeated: a group that should read everything a team owns is one org-level grant, not one grant per project.

Organizations never appear in URLs. A project's address is `/project/{slug}/` whatever organization it belongs to, so moving a project between organizations breaks no links, and an organization's slug can never collide with a project's.

Installations that predate organizations have all their projects in one called **No Org**. It is an ordinary organization: rename it, grant on it, or move projects out of it.

Pick a project's organization when you create it, or change it later on its edit page. The front page groups projects by organization, with a heading you can click to show only that one; the box beside it filters the list, and accepts a partly typed name.

## Exposure: Reaching Beyond Grants

Grants say who is let in. **Exposure**, on the project's edit page, says how far the project reaches beyond them:

| Exposure | Who can read it |
|---|---|
| **Public** | Anyone, including signed-out visitors |
| **Any signed-in user** | Everyone with an account |
| **Only who is granted access** | Exactly the grants, and nothing else |

Only an instance admin can make a project public.

Exposure never restricts a grant — someone granted editor can still upload to a public project — and grants never widen exposure. They answer different questions.

## Instance Roles

A user's own role, on **Admin > Users**, sits above all of this:

- **admin** — everything, everywhere. Grants do not constrain them.
- **editor** — may upload to any project, including ones they cannot read. This asymmetry predates the access model and is preserved deliberately; give an editor a viewer grant if they should also see it.
- **viewer** — nothing beyond what grants give them.

Creating a project makes you an admin of it, recorded as an ordinary grant. You can see it in the project's Access table, and revoke it like any other.

## Declaring Access in config.yaml

Everything above can be declared in `config.yaml` instead of clicked, which is
what you want when the instance is provisioned rather than administered:

```yaml
access:
  groups:
    - name: engineering
      description: Dev team
      members:
        - ldap_group: "cn=eng,ou=groups,dc=example,dc=com"
        - user: alice
  grants:
    - group: engineering
      org: default
      role: viewer
    - group: engineering
      project: docs
      role: editor
```

What the file declares, the file owns. Those rows are reconciled against it on
every startup, so **deleting an entry revokes it** — config is declarative, not
additive. Anything added through the admin UI is left alone, so the two can
coexist: provision the baseline in the file, handle exceptions in the UI.

An entry naming a project, group or user that does not exist is skipped and
logged, not fatal — one typo does not stop the server.

Two older keys are retired. `auth.ldap.project_groups` and
`auth.oauth2.project_groups` are still applied, translated into a group plus a
grant, and warn at startup. `access.private` is **not** applied: it granted
access to every "private"-visibility project, and that visibility no longer
exists. Existing installations had its members migrated once into a group
called *Private Access* — grant that group where you want it.

## Worked Example

An engineering team that writes some docs and reads others:

1. **Admin > Access Groups** → create `engineering`, add the LDAP group `cn=eng,dc=example,dc=com`.
2. **Admin > Organizations** → on the organization holding your projects, grant `engineering` **viewer**. Everyone in it can now read everything there.
3. On the two projects the team owns, grant `engineering` **editor**. The stronger role wins on those, and stays viewer elsewhere.

One group, three grants, no duplication — and adding a person to the LDAP group is the only step for the next hire.
