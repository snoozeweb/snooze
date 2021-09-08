<template>
  <div>
    <b-card no-body header="Infos" header-class='text-center font-weight-bold'>
    <b-card-body class="p-0">
      <div>
      <b-table
        :items="infos"
        :fields="fields"
        thead-class="d-none"
        class='m-0'
        borderless
        small
      >
      </b-table>
      </div>
    </b-card-body>
    </b-card>
  </div>
</template>

<script>
export default {
  name: 'Info',
  components: {
  },
  props: {
    // Object being represented
    myobject: {type: Object},
    // List of object property to exclude from the view
    excluded_fields: {type: Array, default: () => []},
  },
  data () {
    return {
      fields: [
        {key: 'name'},
        {key: 'value', tdClass: 'border-left, multiline, text-break'},
      ],
    }
  },
  computed: {
    infos () {
      return Object.keys(this.myobject)
        .filter(key => !this.excluded_fields.includes(key) && key[0] != '_')
        .reduce((obj, key) => {
          obj.push({name: key, value: this.myobject[key]})
          return obj
        }, [])
    }
  },
}
</script>
