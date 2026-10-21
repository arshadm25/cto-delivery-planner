# CSV export to Redmine API mapping

Backlog item P1-08. How each column of a Redmine CSV issue export maps to the REST API, and how `csv-compare` treats it.

The mapping was checked against two real exports (568 rows, 29 columns, identical headers). No export data is stored in this repository.

## Export settings that matter

| Setting | What we saw | Why it matters |
|---|---|---|
| Interface language | English headers | `csv-compare` matches columns by English name and refuses other languages |
| Date format | `DD-MM-YYYY` | Default `-date-format 02-01-2006` |
| Time format | 12-hour, `05:45 PM` | Default `-time-format "03:04 PM"` |
| Time zone | Not stated in the file | Timestamps are in the exporting user's Redmine time zone (My account › Preferences). Pass it with `-tz` |
| Encoding | One file was not valid UTF-8 | Only affects free-text columns, which are not read. Export as UTF-8 next time if possible |
| Row limit | One file had exactly 500 rows | Redmine's CSV export has a configurable row limit (Administration › Settings › Issue tracking). A file that stops exactly at the limit has probably been cut off |
| Status filter | Almost no rows had a Closed date | The exports were most likely filtered to open issues, so closed issues will show as "in API, missing from CSV" |

## Column mapping

| CSV column | API field (`/issues.json`) | Compared | Notes |
|---|---|---|---|
| `#` | `id` | Key | Join key between CSV and API |
| `Project` | `project.id` | Yes | CSV has the project name; matched to `projects.csv` by name, then identifier |
| `Tracker` | `tracker.name` | Yes | |
| `Parent task` | `parent.id` | Yes | |
| `Parent task subject` | – | No | Free text |
| `Status` | `status.name` | Yes | Open or closed comes from `/issue_statuses.json`, not from the name |
| `Priority` | `priority.name` | Yes | |
| `Subject` | `subject` | No | Free text, never read |
| `Author` | `author.id` | No | Person |
| `Assignee` | `assigned_to.id` | Set or empty only | Names are never compared or written out |
| `Updated` | `updated_on` | Used to classify | Minute precision in local time; API is UTC with seconds |
| `Category` | `category` | No | Empty in both exports |
| `Target version` | `fixed_version.name` | Yes | |
| `Start date` | `start_date` | Yes | Normalised to `YYYY-MM-DD` |
| `Due date` | `due_date` | Yes | Blank stays unknown |
| `Estimated time` | `estimated_hours` | Yes | Blank stays unknown, never zero |
| `Total estimated time` | `total_estimated_hours` | No | Includes subtasks; not in the discovery index yet |
| `Spent time` | `spent_hours` | Only when both have a value | The CSV shows `0.00` when nothing is logged |
| `Total spent time` | `total_spent_hours` | No | Includes subtasks |
| `% Done` | `done_ratio` | Yes | |
| `Created` | `created_on` | No | Parsed and validated only |
| `Closed` | `closed_on` | Set or empty | |
| `Last updated by` | – | No | Person |
| `Related issues` | `/issues/{id}/relations.json` | No | Relations come in Phase 2 (P2-04) |
| `Files` | attachments | No | |
| `Story Points` | custom field | No | Empty in both exports |
| `Private` | `is_private` | When both have a value | Private issues need the matching permission for the service account to see them |
| `Description` | `description` | No | Free text, never read |
| `Last notes` | journals | No | Free text, never read |

## How differences are classified

| Category | Meaning |
|---|---|
| Differs, changed after export | A field differs and the API `updated_on` is later than the CSV `Updated` value. Expected; not an error |
| Differs, not explained by a later edit | A field differs but the API copy is not newer. Investigate |
| In CSV, missing from API | The service account cannot see the issue (membership, private issue), or it was deleted |
| In API, missing from CSV | Usually closed issues or rows past the export limit. The report gives the likely reason |
| Duplicate ID in CSV | The same ID in more than one row or file |

## Sensitive content in free-text columns

Issue descriptions and notes can contain things that must not be copied around, such as login details pasted into a ticket. The planner should not copy `Description` or `Last notes` into its database unless a later decision requires it (section 8 of the implementation plan marks `description` as "only if needed").
