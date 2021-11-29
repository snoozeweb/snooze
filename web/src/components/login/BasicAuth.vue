<template>
  <CForm @submit="onSubmit">
    <div v-if="endpoint != 'anonymous'">
      <p class="text-muted">Sign In to your account</p>
      <CInputGroup class="mb-3">
        <CInputGroupText><i class="la la-user la-lg"></i></CInputGroupText>
        <CFormInput type="text" v-model="username" placeholder="Username" autocomplete="username email" @input="reset_validation" required/>
      </CInputGroup>
      <CInputGroup class="mb-4">
        <CInputGroupText><i class="la la-lock la-lg"></i></CInputGroupText>
        <CFormInput type="password" v-model="password" placeholder="Password" autocomplete="current-password" @input="reset_validation" required/>
        <div v-if="!validate" class="w-100 text-danger mt-1" style="font-size: 0.875rem;">
          Error: {{ error_message }}
        </div>
      </CInputGroup>
    </div>
    <CRow>
      <CCol col="6">
        <CButton type="submit" color="primary" class="px-4">Login</CButton>
      </CCol>
    </CRow>
  </CForm>
</template>
<script>
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
              this.$router.push(decodeURIComponent(this.$route.query.return_to))
            } else if (response.data.default_page) {
              this.$router.push(response.data.default_page)
            } else {
              this.$router.push('/record')
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
          this.validate = false
          this.error_message = error.description
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
