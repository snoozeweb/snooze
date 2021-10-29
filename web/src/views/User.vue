<template>
  <div class="animated fadeIn">
    <List
      endpoint_prop="user"
      delete_mode
      ref="list"
    >
      <template #head_buttons>
        <b-button variant="success" @click="modal_add()">New</b-button>
      </template>
      <template #button="row">
        <b-button size="sm" @click="modal_edit(row.item)" variant="primary" v-b-tooltip.hover title="Edit"><i class="la la-pencil-alt la-lg"></i></b-button>
      </template>
    </List>
  </div>
</template>

<script>
import List from '@/components/List.vue'

export default {
  components: {
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
