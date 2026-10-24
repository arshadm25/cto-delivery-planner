import { expect, Page, test } from "@playwright/test";

const PASSWORD = "demo-password-123";

async function login(page: Page, email: string) {
  await page.goto("/login");
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password").fill(PASSWORD);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("button", { name: "Log out" })).toBeVisible();
}

test("baseline is proposed, approved, and kept separate from the forecast", async ({ page, browser }) => {
  await login(page, "pm@example.test");
  await page.goto("/projects/10");
  await expect(page.getByText("Approved baseline")).toBeVisible();
  await expect(page.getByText("No approved commitment")).toBeVisible();
  await page.getByRole("tab", { name: "Commitments" }).click();
  await page.getByPlaceholder("Title").fill("Phase 1 go-live");
  await page.getByLabel("Baseline date").fill("2027-03-31");
  await page.getByPlaceholder("Reason or source of the commitment").fill("Fictional signed plan");
  await page.getByRole("button", { name: "Propose baseline" }).click();
  await expect(page.getByRole("cell", { name: "proposed" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Approve" })).toHaveCount(0);

  const cto = await browser.newPage();
  await login(cto, "cto@example.test");
  await cto.goto("/projects/10#commitments");
  await cto.getByRole("button", { name: "Approve" }).click();
  await expect(cto.getByRole("cell", { name: "approved" })).toBeVisible();
  await expect(cto.getByText("31 Mar 2027").first()).toBeVisible();
  await expect(cto.getByText("Forecast", { exact: true }).first()).toBeVisible();
  await expect(cto.getByText("Actual", { exact: true }).first()).toBeVisible();
});

test("weekly report and workload pages render", async ({ page }) => {
  await login(page, "cto@example.test");
  await page.getByRole("link", { name: "Weekly report" }).click();
  await expect(page.getByRole("heading", { name: "Decisions needed" })).toBeVisible();
  await page.getByRole("link", { name: "Workload" }).click();
  await expect(page.getByText("This view is for planning capacity, not for comparing people.")).toBeVisible();
});

test("work is planned in the planner without touching Redmine", async ({ page }) => {
  await login(page, "pm@example.test");
  await page.goto("/projects/10#milestones");
  await page.getByLabel("Milestone name").fill("Phase 2 (planner)");
  await page.getByLabel("Milestone due date").fill("2027-02-26");
  await page.getByRole("button", { name: "Add milestone" }).click();
  await expect(page.getByRole("cell", { name: /Phase 2 \(planner\)/ })).toBeVisible();

  // Move two issues into the new milestone in one go.
  await page.getByRole("tab", { name: /Issues/ }).click();
  const boxes = page.getByRole("checkbox", { name: /Select issue/ });
  await boxes.nth(0).check();
  await boxes.nth(1).check();
  await expect(page.getByText("2 selected")).toBeVisible();
  await page.getByLabel("Bulk milestone").selectOption({ label: "Phase 2 (planner)" });
  await page.getByRole("button", { name: "Apply to 2" }).click();
  await expect(page.getByText("PLANNER", { exact: true }).or(page.getByText("planner", { exact: true }))).not.toHaveCount(0);

  // Set one estimate inline.
  await page.getByRole("button", { name: "Edit", exact: true }).last().click();
  await page.getByLabel("Estimate hours").fill("12");
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("cell", { name: /12(\.0)? ?h/ }).first()).toBeVisible();

  // Clicking a milestone opens its issues in the planner, not Redmine.
  await page.getByRole("tab", { name: /Milestones/ }).click();
  await page.getByRole("button", { name: "Phase 2 (planner)" }).click();
  await expect(page.getByRole("tab", { name: /Issues/ })).toHaveAttribute("aria-selected", "true");
  await expect(page.getByLabel("Milestone filter")).toHaveValue(/^-\d+$/);
  await expect(page.getByRole("checkbox", { name: /Select issue/ })).toHaveCount(2);
});

test("a team is built from Redmine members", async ({ page }) => {
  await login(page, "pm@example.test");
  await page.goto("/capacity");
  await page.getByRole("button", { name: "New team" }).click();
  await page.getByLabel("Team name").fill("Fictional squad");
  await page.getByLabel("Member to add").selectOption({ index: 1 });
  await page.getByRole("button", { name: "Add member" }).click();
  await page.getByLabel(/Hours per week for/).fill("30");
  await page.getByRole("button", { name: "Save team" }).click();
  await expect(page.getByRole("cell", { name: "Fictional squad" })).toBeVisible();
  await page.getByLabel("Person or team").selectOption({ label: "Fictional squad" });
});

test("a planner task is added, finished, and capacity is given from the project", async ({ page }) => {
  await login(page, "pm@example.test");
  await page.goto("/projects/10#issues");
  await page.getByRole("button", { name: "Add a task" }).click();
  await page.getByLabel("Task subject").fill("Fictional client sign-off");
  await page.getByLabel("Task estimate").fill("3");
  await page.getByRole("button", { name: "Add task" }).click();
  const row = page.getByRole("row", { name: /Fictional client sign-off/ });
  await expect(row).toBeVisible();
  await row.getByRole("button", { name: "Done" }).click();
  await expect(page.getByRole("row", { name: /Fictional client sign-off/ })).toHaveCount(0);

  await page.getByRole("tab", { name: "Capacity" }).or(page.getByRole("button", { name: "Capacity" })).first().click();
  await expect(page.getByText("Capacity for this project")).toBeVisible();
  await page.getByLabel("Who").selectOption({ index: 1 });
  await page.getByLabel("From").fill("2026-11-02");
  await page.getByLabel("To").fill("2026-11-27");
  await page.getByLabel("Hours on this project").fill("40");
  await page.getByRole("button", { name: "Save draft" }).click();
  await expect(page.getByText("draft", { exact: true }).first()).toBeVisible();
});
