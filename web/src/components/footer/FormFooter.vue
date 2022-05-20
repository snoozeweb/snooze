<template>
  <div>
    <div>
      <CRow class="align-items-center">
        <CCol sm="auto">
          <CButton class="me-2" color="secondary" size="sm" href="#" @click="visible = !visible">
            <i v-if="visible" class="la la-angle-up la-lg"></i>
            <i v-else class="la la-angle-down la-lg"></i>
          </CButton>
        </CCol>
        <CCol sm="auto" class="px-1">
          <h5 class="m-0">
            <label :id="'title_' + metadata.display_name" v-c-tooltip="{content: this.metadata.description, placement: 'right'}">{{ metadata.display_name }}</label>
          </h5>
        </CCol>
        <CCol sm="auto" style="padding-top:0.1rem">
          <label v-if="feedback" class="fst-italic">
            {{ feedback }} hit(s)
          </label>
          <label v-else class="fst-italic">
            No hits
          </label>
        </CCol>
      </CRow>
    </div>
    <div>
      <CCollapse :visible="visible" class="pt-2">
        <component
          :id="'component_'+metadata.display_name"
          :is="component"
          :data="data"
          @feedback="on_feedback"
        />
      </CCollapse>
    </div>

  </div>
</template>

<script>
import { defineAsyncComponent, shallowRef } from 'vue'
// @group Forms
// Base class for all form inputs
export default {
  emits: ['update:modelValue'],
  props: {
    metadata: {type: Object, default: () => {}},
    data: {type: Object},
  },
  data() {
    return {
      component: shallowRef(defineAsyncComponent(() => import(`./${this.metadata.component}.vue`))),
      visible: false,
      feedback: '',
    }
  },
  methods: {
    on_feedback(val) {
      this.feedback = val
    },
  },
}

</script>
