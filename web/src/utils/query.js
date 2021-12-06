// Transform an object {key => value} to a query key=value&...
// Initial `?` is not included, but the values are URL-escaped
export function object_to_query(obj) {
  return Object.entries(obj).map(([key, val]) => `${key}=${encodeURIComponent(val)}`).join('&')
}

// Join multiple queries in snooze with a `AND`
// Will catch edge case when a query is empty string
// or empty array.
// Example:
// Input: [ ['=', 'a', '1'], ['=', 'b', '2'] ]
// Output: ['AND', ['=', 'a', '1'], ['=', 'b', '2']]
export function join_queries(original_queries, op="AND") {
  var queries = Object.assign([], original_queries)
  if (queries.length == 0) {
    return []
  }
  return queries.reduce((memo, query) => {
    if (query != null && query != "" && query != []) {
      return [op, memo, query]
    } else {
      return memo
    }
  })
}
