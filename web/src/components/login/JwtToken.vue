<template>
  <CForm>
    <p class="text-muted">Please enter your JWT token</p>
    <CInputGroup class="mb-4">
      <CInputGroupText><i class="la la-lock la-lg"></i></CInputGroupText>
      <CFormInput v-model="token" type="password" class="login" placeholder="JWT Token" @input="reset_validation"/>
      <div v-if="!validate" class="w-100 text-danger mt-1" style="font-size: 0.875rem;">
        Error: {{ error_message }}
      </div>
    </CInputGroup>
    <CRow>
      <CCol col="6">
        <CButton v-on:click="login" color="primary" class="px-4">Login</CButton>
      </CCol>
    </CRow>
  </CForm>
</template>
<script>
import { safe_jwt_decode } from '@/utils/api'

export default {
  name: 'JwtToken',
  data () {
    return {
      token: "",
      validate: true,
      error_message: "",
    }
  },
  methods: {
    login: function () {
      console.log("Got authentication token")
      var decoded_token = safe_jwt_decode(this.token, false)
      if (decoded_token) {
        localStorage.setItem('snooze-token', this.token)
        this.$router.push('/record')
      } else {
        this.error_message = 'Invalid token'
        this.validate = false
      }
    },
    reset_validation() {
      this.validate = true
      this.error_message = ''
    }
  },
}
</script>
