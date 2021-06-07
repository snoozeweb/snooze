import { API } from '@/api'
import moment from 'moment'

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

export function preprocess_data(data) {
  var filtered_object = Object.assign({}, data)
  Object.keys(filtered_object).forEach((key, ) => {
    if (key[0] == '_') {
      delete filtered_object[key]
    }
  })
  return filtered_object
}

export function add_items(endpoint, items, callback = null, callback_arguments = null) {
  items = items.map(item => preprocess_data(item))
  console.log(`POST ${endpoint}`)
  console.log(items)
  API
    .post(`/${endpoint}`, items)
    .then(response => {
      console.log(response)
      if (callback) {
        callback(response, callback_arguments)
      }
    })
    .catch(error => console.log(error))
}

export function update_items(endpoint, items, callback = null, callback_arguments = null) {
  items = items.map(item => preprocess_data(item))
  console.log(`PUT ${endpoint}`)
  console.log(items)
  API
    .put(`/${endpoint}`, items)
    .then(response => {
      console.log(response)
      if (callback) {
        callback(response, callback_arguments)
      }
    })
    .catch(error => console.log(error))
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

export function pp_countdown(secs) {
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
  } else {
    output = '0s'
  }
  return output
}

export function countdown(secs) {
  var sec_num = parseInt(secs, 10)
  var hours   = Math.floor(sec_num / 3600)
  var minutes = Math.floor(sec_num / 60) % 60
  var seconds = sec_num % 60
  if (seconds < 0) {
    return '00:00:00'
  } else {
    if (hours < 10) {
      hours = "0" + hours;
    }
    if (minutes < 10) {
      minutes = "0" + minutes;
    }
    if (seconds < 10) {
      seconds = "0" + seconds;
    }
    return hours + ':' + minutes + ':' + seconds
  }
}
export function trimDate(date, splitH) {
  if (!date) {
    return 'Empty'
  }
  var mDate = moment(date)
  var newDate = ''
  var now = moment()
  if (mDate.year() == now.year()) {
    if (mDate.format('MM-DD') == now.format('MM-DD')) {
      if(splitH) {
        newDate = 'Today' + '\n' + mDate.format('HH:mm:ss')
      } else {
        newDate = 'Today' + ' ' + mDate.format('HH:mm:ss')
      }
    } else {
      newDate = mDate.format('MMM Do HH:mm:ss')
    }
  } else {
    newDate = mDate.format('MMM Do YYYY')
  }
  if(splitH) {
    var splitDate = newDate.split(' ')
    if (splitDate.length > 2) {
      newDate = splitDate[0] + ' ' + splitDate[1] + '\n' + splitDate.slice(2).join(' ')
    }
  }
  return newDate
}
