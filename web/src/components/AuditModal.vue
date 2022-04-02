<template>
  <CModal
    ref="diff"
    :visible="visible"
    @close="clear"
    alignment="center"
    backdrop="static"
    size="xl"
  >
    <CModalHeader :class="`bg-${auditLog.color}`">
      <CModalTitle class="text-white">
        {{ auditLog.name }} by {{ auditLog.method }}/{{ auditLog.username }}
        @ <DateTime :date="auditLog.timestamp" show_secs="true" /> ({{ auditLog.timestamp }})
      </CModalTitle>
    </CModalHeader>
    <CModalBody>
      <CRow>
        <CCol xs="auto">
          <ul>
            <li><b>Source IP:</b> {{ auditLog.source_ip }}</li>
            <li><b>User agent:</b> {{ auditLog.user_agent }}</li>
          </ul>
        </CCol>
      </CRow>
      <CRow>
        <code-diff
          class="mt-2"
          v-if="diffComputed"
          :old-string="stringBefore"
          :new-string="stringAfter"
          file-name="diff"
          :output-format="diffConfig.style"
          :context="diffConfig.context"
        />
      </CRow>
      <CCard no-body v-if="auditLog.error || auditLog.traceback">
        <CCardBody>
        <template v-if="auditLog.error">
          <h3>Error message</h3>
          <p>{{ auditLog.error }}</p>
        </template>
        <template v-if="auditLog.traceback">
          <h3>Traceback</h3>
          <p>
            <template v-for="line in auditLog.traceback">{{ line }}</template>
          </p>
        </template>
        </CCardBody>
      </CCard>
    </CModalBody>
    <CModalFooter>
      <CRow>
        <CCol xs="auto">
          <CInputGroup>
            <CInputGroupText>Format</CInputGroupText>
            <CFormSelect id="diffFormat" v-model="diffConfig.format" :value="diffConfig.format" size="sm">
              <option v-for="opt in FORMAT_OPTIONS" :value="opt">{{ opt }}</option>
            </CFormSelect>
          </CInputGroup>
        </CCol>
        <CCol xs="auto">
          <CInputGroup>
            <CInputGroupText>Style</CInputGroupText>
            <CFormSelect id="diffStyle" v-model="diffConfig.style" :value="diffConfig.style" size="sm">
              <option v-for="opt in STYLE_OPTIONS" :value="opt">{{ opt }}</option>
            </CFormSelect>
          </CInputGroup>
        </CCol>
        <CCol xs="auto">
          <CInputGroup>
            <CInputGroupText>Context lines</CInputGroupText>
            <CFormInput type="number" v-model="diffConfig.context" />
          </CInputGroup>
        </CCol>
      </CRow>
      <CButton @click="clear" color="secondary">Close</CButton>
    </CModalFooter>
  </CModal>
</template>

<script>

const FORMAT_OPTIONS = ['yaml', 'json']
const STYLE_OPTIONS = ['side-by-side', 'line-by-line']

const yaml = require('js-yaml')
import { CodeDiff } from 'v-code-diff'

import DateTime from '@/components/DateTime.vue'
import { get_data } from '@/utils/api'

export default {
  name: 'AuditModal',
  components: {
    DateTime,
    CodeDiff,
  },
  props: {
    collection: {type: String},
    objectId: {type: String},
    auditLogs: {type: Array},
  },
  created () {
    this.FORMAT_OPTIONS = FORMAT_OPTIONS
    this.STYLE_OPTIONS = STYLE_OPTIONS
  },
  mounted () {
    var config = localStorage.getItem('diffConfig')
    if (config) {
      console.log('Custom diffConfig found in localStorage')
      this.diffConfig = JSON.parse(config)
    }
  },
  data () {
    return {
      index: null,
      visible: false,
      diffComputed: false,
      modalBefore: {},
      modalAfter: {},
      stringBefore: null,
      stringAfter: null,
      diffConfig: {
        format: 'yaml',
        context: 10,
        style: 'line-by-line'
      },
    }
  },
  methods: {
    // Serialize an object to the configured diff format
    serialize(obj) {
      if (this.diffConfig.format == 'yaml') {
        return yaml.dump(obj)
      } else if (this.diffConfig.format == 'json') {
        return JSON.stringify(obj, null, 2)
      } else {
        console.log(`diffConfig: ${JSON.stringify(this.diffConfig)}`)
        throw "Unsupported diff format"
      }
    },
    // Serialize the before and after object, effectively computing the diff
    serializeBeforeAfter() {
      try {
        this.stringBefore = this.serialize(this.modalBefore.snapshot)
        this.stringAfter = this.serialize(this.modalAfter.snapshot)
      } catch(error) {
        console.error(error)
        console.log('modalBefore', this.modalBefore)
        console.log('modalAfter', this.modalAfter)
      }
      this.diffComputed = true
    },
    // Query the most recent audit log which timestamp is strictly lower than `from`
    queryAudit(from) {
      var query = ['AND',
        ['=', 'object_id', this.objectId],
        ['<', 'timestamp', from],
        ['!=', 'action', 'rejected'],
      ]
      var options = {
        asc: false,
        orderby: 'timestamp',
      }
      return get_data('audit', query, options, (response) => {
        if (response.data && response.data.data && response.data.data[0]) {
          var auditLog = response.data.data[0]
          console.log(`[QUERY] Found previous audit log: ${auditLog.uid}`)
          return auditLog
        } else {
          throw `Could not find audit log for ${query}`
        }
      })
    },
    // Compute the diff for an object at a given index
    setBeforeAfter(index) {
      console.log(`index=${index}, auditLogs.length=${this.auditLogs.length}`)

      this.index = index
      var auditLog = this.auditLogs[index]
      var previousAudit = this.auditLogs.slice(index+1).find(a => a.action != 'rejected')

      if (auditLog.action == 'added') {
        this.modalBefore = {}
        this.modalAfter = auditLog
        this.serializeBeforeAfter()
      } else if (previousAudit) {
        this.modalBefore = previousAudit
        this.modalAfter = auditLog
        this.serializeBeforeAfter()
      } else {
        this.queryAudit(auditLog.timestamp)
        .then(data => {
          console.log('queryAudit.data', data)
          this.modalBefore = data
          this.modalAfter = auditLog
          this.serializeBeforeAfter()
        })
        .catch(error => {
          this.modalBefore = {}
          this.modalAfter = auditLog
          this.serializeBeforeAfter()
        })
      }
    },
    show(index) {
      console.log(`AuditModal.show(${index})`)
      this.setBeforeAfter(index)
      this.visible = true
    },
    // Clear the modal
    clear() {
      console.log("AuditModal.clear()")
      this.visible = false
      this.modalBefore = null
      this.modalAfter = null
      this.index = null
      this.diffComputed = false
      Array.from(document.getElementsByClassName('modal')).forEach(el => el.style.display = "none")
      Array.from(document.getElementsByClassName('modal-backdrop')).forEach(el => el.style.display = "none")
    },
  },
  computed: {
    // Used to show the current audit log in the modal
    auditLog() {
      if (this.index !== null) {
        return this.auditLogs[this.index]
      } else {
        return {}
      }
    },
  },
  watch: {
    // Save the configuration to localStorage when modified
    diffConfig: {
      handler: function() {
        console.log('Updated diffConfig in localStorage')
        localStorage.setItem('diffConfig', JSON.stringify(this.diffConfig))
        this.serializeBeforeAfter()
      },
      deep: true,
    },
  },
}

</script>
