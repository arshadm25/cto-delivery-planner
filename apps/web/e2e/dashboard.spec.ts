import { expect, Page, test } from "@playwright/test";

const PASSWORD = "demo-password-123";

async function login(page: Page, email: string) {
  await page.goto("/login");
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password").fill(PASSWORD);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("button", { name: "Log out" })).toBeVisible();
}

test("anonymous visitors are sent to login", async ({ page }) => {
  await page.goto("/");
  await expect(page).toHaveURL(/\/login/);
});

test("wrong password is rejected", async ({ page }) => {
  await page.goto("/login");
  await page.getByLabel("Email").fill("cto@example.test");
  await page.getByLabel("Password").fill("not the password");
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByText("invalid email or password")).toBeVisible();
});

test("CTO sees the portfolio overview", async ({ page }) => {
  await login(page, "cto@example.test");
  await expect(page.getByRole("heading", { name: "Portfolio overview" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Alpha Portal" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Alpha Mobile" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Internal Admin" })).toHaveCount(0);
});

test("project manager sees only the assigned project", async ({ page }) => {
  await login(page, "pm@example.test");
  await expect(page.getByRole("link", { name: "Alpha Portal" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Alpha Mobile" })).toHaveCount(0);
  await page.goto("/projects/11");
  await expect(page.getByText("project not found")).toBeVisible();
});

test("project page reports unknowns as TBD and links flags to Redmine", async ({ page }) => {
  await login(page, "cto@example.test");
  await page.getByRole("link", { name: "Alpha Portal" }).click();
  await expect(page.getByText("Owner:")).toContainText("TBD");
  await expect(page.getByText("Missing data that limits forecast confidence")).toBeVisible();
  const link = page.locator('a[href^="https://redmine.example.test/issues/"]').first();
  await expect(link).toBeVisible();
});

test("sync status screen shows the last run", async ({ page }) => {
  await login(page, "cto@example.test");
  await page.getByRole("link", { name: "Sync status" }).click();
  await expect(page.getByRole("heading", { name: "Recent runs" })).toBeVisible();
  await expect(page.getByRole("cell", { name: "succeeded" }).first()).toBeVisible();
});

test("viewer cannot see admin pages", async ({ page }) => {
  await login(page, "viewer@example.test");
  await expect(page.getByRole("link", { name: "Users" })).toHaveCount(0);
  await page.goto("/admin/users");
  await expect(page.getByText("your role does not allow this")).toBeVisible();
});
