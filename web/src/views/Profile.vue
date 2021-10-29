<template>
  <div class="animated fadeIn">
    <Card
      endpoint_prop="profile"
      :onSubmit="submit"
      :loaded_callback="loaded_callback"
      ref="card"
    >
    </Card>
  </div>
</template>

<script>

import Card from '@/components/Card.vue'
import nav from '@/containers/_nav'

export default {
  components: {
    Card,
  },
  methods: {
    submit(data) {
      this.$refs.card.submit(data, this[this.$refs.card.current_tab.callback])
    },
    reload_profile() {
      console.log("Reload profile")
      localStorage.setItem('refreshed', false)
    },
    loaded_callback() {
      delete this.$refs.card.form['general']['password']
      if (localStorage.getItem('method') == 'local') {
        this.$refs.card.form['general']['password'] = {
          display_name: 'Reset Password',
          component: 'Password',
          description: 'Reset password'
        }
      }
      this.$refs.card.form.preferences.default_page.options = []
      nav[0]._children.forEach(n => {
        if (n._name == "CSidebarNavItem") {
          this.$refs.card.form.preferences.default_page.options.push({text: n.name, value: n.to})
        }
      })
    },
  },
}
</script>
