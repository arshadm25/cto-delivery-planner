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
