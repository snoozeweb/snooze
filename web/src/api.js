import axios from 'axios'
import router from '@/router'
import jwt_decode from "jwt-decode"

axios.defaults.headers.post['Content-type'] = 'application/json'

if ('VUE_APP_API' in process.env) {
  var axios_config = {baseURL: process.env.VUE_APP_API}
} else {
  var axios_config = {baseURL: '/api/'}
}

export const API = axios.create(axios_config)

if (!localStorage.getItem('username')) {
  let token = localStorage.getItem('snooze-token')
  if (token) {
    localStorage.setItem('username', jwt_decode(token).username)
  }
}

// Compute the value of return_to with the current route
// @returns {string} The value of the URL to return to after login
function compute_return_to() {
  var return_to
  const route = router.currentRoute.value
  if (route.query.return_to) {
    return_to = route.query.return_to
  } else {
    return_to = encodeURIComponent(route.fullPath)
  }
  return return_to
}

// Token authentication
API.interceptors.request.use(
  function (config) {
    let token = localStorage.getItem('snooze-token')
    if (token) {
      config.headers['Authorization'] = "JWT " + token
    } else {
      const return_to = compute_return_to()
      router.push(`/login?return_to=${return_to}`)
    }
    return config
  },
  (error) => { return Promise.reject(error) }
)

// Redirect to login on 401
API.interceptors.response.use(
  function (resp) { return resp },
  function (error) {
    if (router.currentRoute.value.path != '/login') {
      if (error.response && error.response.status === 401) {
        const return_to = compute_return_to()
        router.push(`/login?return_to=${return_to}`)
      } else {
        return error
      }
    } else {
      return error
    }
  }
)
