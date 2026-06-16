/** Returns the alert search string for a given time-series bucket.
 *  x is the ISO timestamp of the bucket start; bucket is the bucket size in seconds. */
export function alertsSearchForBucket(x: string, bucket: number): string {
  const from = Math.floor(Date.parse(x) / 1000);
  const to = from + bucket;
  return `date_epoch > ${from} and date_epoch < ${to}`;
}

/** Returns the alert search string spanning a dragged range of buckets.
 *  fromX / toX are the ISO timestamps of the first and last selected bucket
 *  starts; bucket is the bucket size in seconds. The upper bound adds one
 *  bucket so the last selected bucket is fully included. When fromX === toX
 *  this is identical to alertsSearchForBucket (a single-bucket selection). */
export function alertsSearchForRange(fromX: string, toX: string, bucket: number): string {
  const from = Math.floor(Date.parse(fromX) / 1000);
  const to = Math.floor(Date.parse(toX) / 1000) + bucket;
  return `date_epoch > ${from} and date_epoch < ${to}`;
}
