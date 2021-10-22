<template>
  <b-form @submit="onSubmit">
    <div v-if="endpoint != 'anonymous'">
      <p class="text-muted">Sign In to your account</p>
      <b-input-group class="mb-3">
        <b-input-group-prepend><b-input-group-text><i class="la la-user la-lg"></i></b-input-group-text></b-input-group-prepend>
        <b-form-input type="text" v-model="username" class="form-control" placeholder="Username" autocomplete="username email" @input="reset_validation" required/>
      </b-input-group>
      <b-input-group class="mb-4">
        <b-input-group-prepend><b-input-group-text><i class="la la-lock la-lg"></i></b-input-group-text></b-input-group-prepend>
        <b-form-input type="password" v-model="password" class="form-control" placeholder="Password" autocomplete="current-password" @input="reset_validation" required/>
        <b-form-invalid-feedback :state="validate">
          {{ error_message }}
        </b-form-invalid-feedback>
      </b-input-group>
    </div>
    <b-row>
      <b-col cols="6">
        <b-button type="submit" variant="primary" class="px-4">Login</b-button>
      </b-col>
    </b-row>
  </b-form>
</template>
<script>
import router from '@/router'
import { API } from '@/api'

export default {
  name: 'BasicAuth',
  props: {
    // The API path to query
    endpoint: {
      type: String,
      required: true,
    },
  },
  data () {
    return {
      token: "",
      username: "",
      password: "",
      validate: true,
      error_message: "",
    }
  },
  methods: {
    onSubmit(event) {
      event.preventDefault()
      API
        .post(`/login/${this.endpoint}`, {}, {"auth": {"username": this.username, "password": this.password}})
        .then(response => {
          if (response.data) {
            console.log("Got authentication token")
            localStorage.setItem('snooze-token', response.data.token)
            if (this.$route.query.return_to) {
              router.push(decodeURIComponent(this.$route.query.return_to))
            } else if (response.data.default_page) {
              router.push(response.data.default_page)
            } else {
              router.push('/record')
            }
          } else {
	    this.validate = false
            if(response.response.status == 401) {
              this.error_message = 'Invalid user or password / Session expired'
            } else {
              this.error_message = response.response.data.description
            }
	  }
        })
        .catch(error => {
          console.log(error)
        })
    },
    reset_validation() {
      this.validate = true
      this.error_message = ''
    }
  },
}
</script>
