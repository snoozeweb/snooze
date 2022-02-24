<template>
  <div class="animated fadeIn">
    <List
      endpoint_prop="user"
      delete_mode
      show_tabs
      ref="list"
    >
      <template #head_buttons>
        <CButton color="success" @click="modal_add()">New</CButton>
      </template>
      <template #custom_buttons="row">
        <CButton size="sm" @click="modal_edit(row.item)" color="primary" v-c-tooltip="{content: 'Edit'}"><i class="la la-pencil-alt la-lg"></i></CButton>
      </template>
      <template #details_side="row">
        <AuditLogs collection="user" :object="row.item" />
      </template>
    </List>
  </div>
</template>

<script>
import AuditLogs from '@/components/AuditLogs.vue'
import List from '@/components/List.vue'

export default {
  components: {
    AuditLogs,
    List,
  },
  methods: {
    modal_add() {
      delete this.$refs.list.form['name']
      delete this.$refs.list.form['password']
      var name_form = {'name': {
          display_name: 'Username',
          component: 'String',
          description: 'Account username',
          required: true
        }
      }
      this.$refs.list.form = Object.assign({}, name_form, this.$refs.list.form);
      this.$refs.list.form['password'] = {
        display_name: 'Password',
        component: 'Password',
        description: 'Set password',
        required: true
      }
      this.$refs.list.modal_add()
    },
    modal_edit(row) {
      delete this.$refs.list.form['name']
      delete this.$refs.list.form['password']
      if (row['method'] == 'local') {
        var name_form = {'name': {
            display_name: 'Username',
            component: 'String',
            description: 'Account username',
            required: true
          }
        }
        this.$refs.list.form = Object.assign({}, name_form, this.$refs.list.form);
        this.$refs.list.form['password'] = {
          display_name: 'Reset Password',
          component: 'Password',
          description: 'Reset password'
        }
      }
      this.$refs.list.modal_edit(row)
    }
  }
}
</script>
