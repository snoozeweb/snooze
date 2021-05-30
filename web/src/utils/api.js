import { API } from '@/api'

import { join_queries, object_to_query, text_alert } from '@/utils/query'

export class ItemAPI {

  constructor(endpoint) {
    this.endpoint = endpoint
  }

  search(query, options) {
    var query_str = object_to_query({s: JSON.stringify(query), ...options})
    var url = `/${this.endpoint}/?${query_str}`
    console.log(`GET ${url}`)
    API
      .get(url)
      .then(response => {
        console.log(response)
        return response.data
      })
      .catch(error => console.log(error))
  }

  delete(items) {
    var uids = items.map(x => x["uid"])
    uids.forEach(uid => {
      console.log(`DELETE ${uid}`)
    })
    var queries = uids.map(uid => ["=", "uid", uid])
    var query = {
      s: JSON.stringify(join_queries(queries, "OR")),
    }
    var query_str = object_to_query(query)
    API
      .delete(`/${this.endpoint}/?${query_str}`)
      .then(response => {
        console.log(response)
        switch(true) {
          case (response.status >= 200 && response.status < 300):
            text_alert(`Deleted ${uids.length} objects`, 'Delete success', 'success')
            break
          default:
            console.log(response)
            text_alert(`Failed to delete ${uids.length} objects: ${response.statusText}`, 'Delete failure', 'danger')
        }
      })
      .catch(error => {
        console.log(error)
      })
  }

}

// Submit data to an endpoint
export function submit(endpoint, data) {
  var filtered_object = Object.assign({}, data)
  Object.keys(filtered_object).forEach((key, ) => {
    if (key[0] == '_') {
      delete filtered_object[key]
    }
  })
  console.log(`POST /${endpoint}`)
  API
    .post(`/${endpoint}`, [filtered_object])
    .then(response => {
      console.log(response)
      text_alert(`Updated object ${response.data["uid"]}`)
    })
    .catch(error => {
      console.log(error)
    })
}

function filter_out_fields(item) {
  var filtered_object = Object.assign({}, item)
  Object.keys(filtered_object).forEach((key, ) => {
    if (key[0] == '_') {
      delete filtered_object[key]
    }
  })
  return filtered_object
}

export function update_items(endpoint, items) {
  //items = items.map(item => filter_out_fields(item))
  console.log(`PUT ${endpoint}`)
  console.log(items)
  API
    .put(`/${endpoint}`, items)
    .then(response => {
      console.log(response)
    })
    .catch(error => console.log(error))
}

export function add_item(endpoint, data) {
}

export function delete_items(endpoint, items) {
  var uids = items.map(x => x["uid"])
  uids.forEach(uid => {
    console.log(`DELETE ${uid}`)
  })
  var queries = uids.map(uid => ["=", "uid", uid])
  var query = {
    s: JSON.stringify(join_queries(queries, "OR")),
  }
  var query_str = object_to_query(query)
  API
    .delete(`/${endpoint}/?${query_str}`)
    .then(response => {
      console.log(response)
      switch(true) {
        case (response.status >= 200 && response.status < 300):
          text_alert(`Deleted ${uids.length} objects`, 'Delete success', 'success')
          break
        default:
          console.log(response)
          text_alert(`Failed to delete ${uids.length} objects: ${response.statusText}`, 'Delete failure', 'danger')
      }
    })
    .catch(error => {
      console.log(error)
    })
}

export function pp_counter(secs) {
  var sec_num = parseInt(secs, 10)
  var hours   = Math.floor(sec_num / 3600)
  var minutes = Math.floor(sec_num / 60) % 60
  var seconds = sec_num % 60

  var output = ''
  if (hours > 0) {
    output += hours + 'h '
  }
  if (minutes > 0) {
    output += minutes + 'm '
  }
  if (seconds > 0) {
    output += seconds + 's'
  }
  return output
}
