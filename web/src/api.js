import axios from 'axios'
import router from './router'
import jwt_decode from "jwt-decode"

axios.defaults.headers.post['Content-type'] = 'application/json'

export const API = axios.create({
  baseURL: process.env.VUE_APP_API,
})

if (!localStorage.getItem('username')) {
  let token = localStorage.getItem('snooze-token')
  if (token) {
    localStorage.setItem('username', jwt_decode(token).user.name)
  }
}

// Token authentication
API.interceptors.request.use(
  function (config) {
    let token = localStorage.getItem('snooze-token')
    if (token) {
      config.headers['Authorization'] = "JWT " + token
    }
    return config
  },
  (error) => { return Promise.reject(error) }
)

// Redirect to login on 401
API.interceptors.response.use(
  function (resp) { return resp },
  function (error) {
    if (router.currentRoute.path != '/login') {
      if (error.response.status === 401) {
        router.push('/login')
      } else {
        return error
      }
    } else {
      return error
    }
  }
)
