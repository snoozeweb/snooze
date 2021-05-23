<template>
  <div class="animated fadeIn">
    <List
      endpoint="user"
      :form="form"
      :fields="fields"
      :hidden_fields="hidden_fields"
      :tabs="tabs"
      :add_mode="false"
      :edit_mode="false"
      ref="list"
    >
      <template #head_buttons>
        <b-button variant="success" @click="modal_add()">Add</b-button>
      </template>
      <template #button="row">
        <b-button size="sm" @click="modal_edit(row.item)" variant="primary"><i class="la la-pencil-square la-lg"></i></b-button>
      </template>
    </List>
  </div>
</template>

<script>
import List from '@/components/List.vue'

import { form, fields, hidden_fields } from '@/objects/User.yaml'

export default {
  components: {
    List,
  },
  mounted () {
  },
  data () {
    return {
      form: form,
      fields: fields,
      hidden_fields: hidden_fields,
      tabs: [
        {title: 'All', filter: []},
        {title: 'LDAP', filter: ["=", "method", "ldap"]},
        {title: 'Local', filter: ["=", "method", "local"]},
      ],
    }
  },
  methods: {
    modal_add() {
      delete this.form['name']
      delete this.form['password']
      var name_form = {'name': {
          display_name: 'Username',
          component: 'String',
          description: 'Username',
          required: true
        }
      }
      this.form = Object.assign({}, name_form, this.form);
      this.form['password'] = {
        display_name: 'Password',
        component: 'Password',
        description: 'Set password',
        required: true
      }
      this.$refs.list.modal_add()
    },
    modal_edit(row) {
      delete this.form['name']
      delete this.form['password']
      if (row['method'] == 'local') {
        var name_form = {'name': {
            display_name: 'Username',
            component: 'String',
            description: 'Username',
            required: true
          }
        }
        this.form = Object.assign({}, name_form, this.form);
        this.form['password'] = {
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
