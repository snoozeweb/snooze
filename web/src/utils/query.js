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
export function join_queries(queries, op="AND") {
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

import { app } from '@/main'

// Alert the user of a problem
export function text_alert(text, title = null, variant = null, position = 'b-toaster-top-right') {
  if (title == null) {
    switch (variant) {
      case 'success':
        title = 'Success!'
        break
      case 'danger':
        title = 'Error!'
        break
      default:
        title = ''
    }
  }
  app.$bvToast.toast(text, {
    title: title,
    variant: variant,
    solid: true,
    toaster: position,
  })
}
