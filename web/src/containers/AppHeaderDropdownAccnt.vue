<template>
  <CDropdown auto-close="outside">
    <CDropdownToggle placement="bottom-end" class="py-0 nav-link" :caret="false">
      <CAvatar>
        <span class="la-stack la-3x">
          <i class="la la-circle la-stack-1x"></i>
          <i class="la la-user-circle la-stack-1x"></i>
        </span>
      </CAvatar>
    </CDropdownToggle>
    <CDropdownMenu class="pt-0">
      <CDropdownHeader component="h6" class="bg-light fw-semibold py-2">
        Account
      </CDropdownHeader>
      <div class="pt-2 px-2">
        <span v-if="display_name" class="text-nowrap large">Hi <strong>{{ display_name }}</strong>!<br/>
          <CDropdownDivider/>
        </span>
        <span class="text-nowrap"><i class="la la-user-circle la-lg"></i> {{ username }}<br/></span>
        <span v-if="email" class="text-nowrap font-italic"><i class="la la-at la-lg"></i> {{ email }}<br/></span>
        <CDropdownDivider/>
        <div class="align-middle">
        <span class="text-nowrap h6">Auth / Roles<br/></span>
        <CBadge :color="get_color(method)" class="me-1">{{ method }}</CBadge>
        <CBadge v-for="field in roles" :key="field" :color="get_color(field)" class="me-1">{{ field }}</CBadge>
        <span v-if="permissions.length > 0">
          <CDropdownDivider />
          <span class="text-nowrap h6">Permissions<br/></span>
          <CBadge v-for="field in permissions" :key="field" :color="get_color(field)" class="me-1">{{ field }}</CBadge>
        </span>
        </div>
      </div>
      <CDropdownDivider/>
      <CDropdownItem class="pointer" @click="logout()">
        <i class="la la-sign-out-alt la-lg pe-2"></i> Logout
      </CDropdownItem>
    </CDropdownMenu>
  </CDropdown>
</template>

<script>
import { API } from '@/api'
import { safe_jwt_decode } from '@/utils/api'
import { get_color } from '@/utils/colors'

export default {
  name: 'AppHeaderDropdownAccnt',
  data () {
    return {
      get_color: get_color,
      username: '',
      roles: [],
      permissions: [],
      method: '',
      display_name: '',
      email: '',
    }
  },
  methods: {
    get_data() {
      API
        .get('/profile_self/general')
        .then(response => {
          if (response.data) {
            this.display_name = response.data['display_name'] || ''
            this.email = response.data['email'] || ''
            localStorage.setItem('display_name', this.display_name)
            localStorage.setItem('email', this.email)
            localStorage.setItem('refreshed', 'true')
          }
        })
        .catch(error => console.log(error))
    },
    logout() {
      localStorage.setItem('snooze-token', '')
      this.$router.go()
    }
  },
  mounted() {
    var token = localStorage.getItem('snooze-token')
    if (token) {
      var decoded_token = safe_jwt_decode(token)
      if (decoded_token) {
        this.username = decoded_token.username
        this.method = decoded_token.method
        this.roles = decoded_token.roles
        this.permissions = decoded_token.permissions
        localStorage.setItem('name', this.username)
        localStorage.setItem('method', this.method)
        localStorage.setItem('roles', this.roles)
        localStorage.setItem('permissions', this.permissions)
      } else {
        return
      }
    }
    if (localStorage.getItem('refreshed') != 'true') {
      this.get_data()
    } else {
      this.display_name = localStorage.getItem('display_name')
      this.email = localStorage.getItem('email')
    }
  }
}
</script>
