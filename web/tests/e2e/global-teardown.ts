// web/tests/e2e/global-teardown.ts
export default async function globalTeardown(): Promise<void> {
  // Workers tear down their own backends; nothing global to clean yet.
}
