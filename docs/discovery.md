# Running Redmine discovery (Phase 1)

Discovery answers one question: which projects and issues can the planner's service account see, and do those totals match the CSV exports? It only sends GET requests and never changes Redmine.

Run it on a trusted machine that can reach Redmine. Claude never needs the API key, and you never need to paste it anywhere.

## 1. Prepare Redmine (backlog items P1-03 to P1-05)

Menu names below match recent Redmine versions. They may differ slightly on yours.

1. **Enable the REST API.** Administration › Settings › API › tick *Enable REST web service* › Save.
2. **Create a read-only role.** Administration › Roles and permissions › New role, for example `Planner read-only`. Grant only view permissions: *View issues*, plus *View time entries* if time data is in scope. Leave every add, edit and delete permission unticked.
3. **Create the service account.** Administration › Users › New user, for example `svc-planner`. Do not make it an administrator.
4. **Add it to each candidate project.** In each project: Settings › Members › New member › `svc-planner` with the `Planner read-only` role. Membership in a parent project does not automatically grant subprojects unless your role and inheritance settings allow it, so check each one.
5. **Get the key.** Sign in as `svc-planner` › My account › API access key › Show. Store it straight into the `.env` file on the trusted host (next step).

## 2. Configure the trusted host

```bash
git clone https://github.com/arshadm25/cto-delivery-planner
cd cto-delivery-planner
cp .env.example .env
chmod 600 .env
# Edit .env: set REDMINE_URL and REDMINE_API_KEY
```

`.env` is git-ignored. Load it into your shell without echoing it:

```bash
set -a; . ./.env; set +a
```

## 3. Test authentication (P1-06)

```bash
make auth-check
```

Expected output: `Authenticated as "svc-planner" (user ID …, administrator: false)`. If it says `administrator: true`, the key belongs to an admin account; create the dedicated account above so the counts reflect what the planner will really see.

## 4. Run discovery (P1-10)

```bash
make discovery
```

This writes three files to `discovery-out/` (git-ignored):

| File | Contents | Share it? |
|---|---|---|
| `report.md` | Totals, warnings, project hierarchy with issue counts, missing-data counts | Yes, in the project thread |
| `projects.csv` | One row per visible project | Yes |
| `issue-index.csv` | One row per issue: IDs, status, dates, estimates. No subjects, notes or names | Yes, for the CSV comparison |

None of the files contain the API key, issue text or people's names. People appear only as Redmine user IDs. Project names are included, so keep the files inside the team.

## 5. What to check in the report

- **Warnings section.** Any total that changed during the run or a fetched count that does not match `total_count` means the run should be repeated before the baseline is accepted.
- **Issues in projects missing from the project list.** Usually closed or archived projects, or projects the account is not a member of.
- **Parent issues not visible.** Usually issues in projects the account cannot see.
- Compare the hierarchy with Administration › Projects to settle open question Q1 (scope).
