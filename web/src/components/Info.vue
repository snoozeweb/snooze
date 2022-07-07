<template>
  <div>
    <CCard>
      <CCardHeader class='card-header-border text-center' style='font-weight:bold'>
        Infos
      </CCardHeader>
      <CCardBody class="p-0">
        <div>
          <CTable
            :items="infos"
            :fields="fields"
            thead-class="d-none"
            class='m-0'
            borderless
            small
            striped
          >
            <CTableBody>
              <CTableRow v-for="(item, i) in infos" :key="i">
                <CTableDataCell scope="row" v-for="(field, k) in fields" :key="`${field.key}_${k}`">{{ item[field.key] }}</CTableDataCell>
              </CTableRow>
            </CTableBody>
          </CTable>
        </div>
      </CCardBody>
    </CCard>
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
