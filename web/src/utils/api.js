import { API } from '@/api'
import moment from 'moment'

import { join_queries, object_to_query, text_alert } from '@/utils/query'

export function get_data(endpoint, query = null, options = {}, callback = null, callback_arguments = null) {
  var query_str = null
  var url = `/${endpoint}`
  if (query) {
    query_str = object_to_query({s: JSON.stringify(query), ...options})
    url = `/${endpoint}?${query_str}`
  }
  console.log(`GET ${url}`)
  API
    .get(url)
    .then(response => {
      console.log(response)
      if (callback) {
        callback(response, callback_arguments)
      }
      if (response.data == undefined) {
        show_feedback(response)
      }
    })
    .catch(error => console.log(error))
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

export function show_feedback(response, title = null, position = 'b-toaster-top-right') {
  if(response.data) {
    if (title) {
      text_alert(`Succeeded to ${title} ${response.data.count || ((response.data.data.replaced || []).length + (response.data.data.updated || []).length + (response.data.data.added || []).length)} object(s)`, title + ' success', 'success', position)
    } else {
      text_alert('Operation successful', 'Success', 'success', position)
    }
  } else {
    var message = ''
    if(response.response && response.response.data.description) {
      message = response.response.data.description
    } else {
      if (title) {
        message = `Failed to ${title} object(s): ${response.statusText}`
      } else {
        message = 'An error occurred'
      }
    }
    if (title) {
      text_alert(message, title + ' failure', 'danger', position)
    } else {
      text_alert(message, 'Failure', 'danger', position)
    }
  }
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
      show_feedback(response, 'Add')
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
      show_feedback(response, 'Update')
    })
    .catch(error => console.log(error))
}

export function delete_items(endpoint, items, callback = null, callback_arguments = null) {
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
    .delete(`/${endpoint}?${query_str}`)
    .then(response => {
      console.log(response)
      if (callback) {
        callback(response, callback_arguments)
      }
      show_feedback(response, 'Delete')
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
  if (secs < 0) {
    output = '0s'
  } else {
    if (hours > 0) {
      output += hours + 'h '
    }
    if (minutes > 0) {
      output += minutes + 'm '
    }
    if (seconds > 0) {
      output += seconds + 's'
    }
  }
  return output
}

export function countdown(secs) {
  var sec_num = parseInt(secs, 10)
  var hours   = Math.floor(sec_num / 3600)
  var minutes = Math.floor(sec_num / 60) % 60
  var seconds = sec_num % 60
  if (secs < 0) {
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

export function trimDate(date, show_secs) {
  if (!date) {
    return 'Empty'
  }
  var mDate = moment(date)
  var newDate = ''
  var now = moment()
  var hours_only = false
  if (!mDate.isValid()) {
    mDate = moment(date, ['HH:mm'])
    hours_only = true
  }
  if (mDate.year() == now.year()) {
    if (mDate.format('MM-DD') == now.format('MM-DD')) {
      if (hours_only) {
        newDate = mDate.format('HH:mm')
      } else if (show_secs) {
        newDate = 'Today' + ' ' + mDate.format('HH:mm:ss')
      } else {
        newDate = 'Today' + ' ' + mDate.format('HH:mm')
      }
    } else {
      if (show_secs) {
        newDate = mDate.format('MMM Do HH:mm:ss')
      } else {
        newDate = mDate.format('MMM Do HH:mm')
      }
    }
  } else {
    newDate = mDate.format('MMM Do YYYY')
  }
  return newDate
}

export function get_weekday(nb) {
  switch(nb) {
    case 1:
      return 'Monday'
    case 2:
      return 'Tuesday'
    case 3:
      return 'Wednesday'
    case 4:
      return 'Thursday'
    case 5:
      return 'Friday'
    case 6:
      return 'Saturday'
    case 7:
      return 'Sunday'
    default:
      return 'Invalid weekday ' + nb
  }
}

export function truncate_message(message, size=280) {
  if (message == null || message.length <= size) {
    return message
  }
  return message.slice(0, size) + '...'
}

export function more(data, limit=5) {
  var lines = data.split(/\r?\n/)
  if (lines.length > limit) {
    var start = lines.slice(0, limit).join('\n')
    var end = lines.slice(limit+1, -1).join('\n')
    return [start, end]
  } else {
    return [data, '']
  }
}

export function to_clipboard(txt) {
  var textArea = document.createElement("textarea");
  textArea.style.position = 'fixed';
  textArea.style.top = 0;
  textArea.style.left = 0;
  textArea.style.width = '2em';
  textArea.style.height = '2em';
  textArea.style.padding = 0;
  textArea.style.border = 'none';
  textArea.style.outline = 'none';
  textArea.style.boxShadow = 'none';
  textArea.style.background = 'transparent';
  textArea.value = txt;

  document.body.appendChild(textArea);
  textArea.focus();
  textArea.select();

  try {
    document.execCommand('copy')
  } catch (err) {
    console.log('Unable to copy');
  }
  document.body.removeChild(textArea);
}

export function pp_number(x) {
  if (isNaN(x)) {
    return '0'
  }
  return x.toString().replace(/\B(?=(\d{3})+(?!\d))/g, ",");
}

export function get_alert_color(type) {
  switch (type) {
    case 'ack':
      return 'success'
    case 'esc':
      return 'warning'
    case 'close':
      return 'tertiary'
    case 'open':
      return 'quaternary'
    default:
      return 'primary'
  }
}

export function get_alert_icon(type) {
  switch (type) {
    case 'ack':
      return 'la-thumbs-up'
    case 'esc':
      return 'la-exclamation'
    case 'close':
      return 'la-lock'
    case 'open':
      return 'la-lock-open'
    default:
      return 'la-comment-dots'
  }
}

export function get_alert_tooltip(type) {
  switch (type) {
    case 'ack':
      return 'Acknowledge'
    case 'esc':
      return 'Re-escalate'
    case 'close':
      return 'Close'
    case 'open':
      return 'Re-open'
    default:
      return 'Comment'
  }
}
