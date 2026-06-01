/** Returns the alert search string for a given time-series bucket.
 *  x is the ISO timestamp of the bucket start; bucket is the bucket size in seconds. */
export function alertsSearchForBucket(x: string, bucket: number): string {
  const from = Math.floor(Date.parse(x) / 1000);
  const to = from + bucket;
  return `date_epoch > ${from} and date_epoch < ${to}`;
}
