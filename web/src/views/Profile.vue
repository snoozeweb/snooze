<template>
  <div class="animated fadeIn">
    <Card
      endpoint="profile_self"
      :tabs="tabs"
      :form="form"
      :onSubmit="submit"
      ref="card"
    >
    </Card>
  </div>
</template>

<script>

import Card from '@/components/Card.vue'
import { tabs, form } from '@/objects/Profile.yaml'
import nav from '@/containers/_nav'

export default {
  components: {
    Card,
  },
  mounted () {
    delete this.form['general']['password']
    if (localStorage.getItem('method') == 'local') {
      this.form['general']['password'] = {
        display_name: 'Reset Password',
        component: 'Password',
        description: 'Reset password'
      }
    }
    this.form.preferences.default_page.options = []
    nav[0]._children.forEach(n => {
      if (n._name == "CSidebarNavItem") {
        this.form.preferences.default_page.options.push({text: n.name, value: n.to})
      }
    })
  },
  data () {
    return {
      tabs: tabs,
      form: form
    }
  },
  methods: {
    submit(data) {
      this.$refs.card.submit(data, this[this.$refs.card.current_tab.callback])
    },
    reload_profile() {
      console.log("Reload profile")
      localStorage.setItem('refreshed', false)
    }
  },
}
</script>
