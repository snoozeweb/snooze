<template>
  <div>
    <div :class="{ 'form-control': true, 'h-auto': true, 'focus': hasFocus, 'is-invalid': required && !checkField, 'is-valid': required && checkField }" @focusin="onFocusin" @focusout="onFocusout">
      <ul class="c-form-tags-list list-unstyled mb-0 d-flex flex-wrap align-items-center" ref="tagsUl">
        <li
          v-for="(tag, index) in tags"
          :key="tag"
          class="c-form-tag d-inline-flex align-items-baseline mw-100"
        >
          <CBadge :color="get_color(colorize?tag:'')" :class="{ 'duplicate': tag === duplicate, [`badge-${size}`]: size }">
            <span>{{ tag }}</span>
            <span v-if="lockedTags.includes(tag)" class="ps-1">
              <i class="la la-lock"></i>
            </span>
            <span v-else>
              <button class="pe-0 delete" @click="removeTag(index)" @click.stop.prevent>
                <i class="la la-times"></i>
              </button>
            </span>
          </CBadge>
        </li>
        <li v-if="!tagsOptions" class="c-form-tags-field flex-grow-1">
          <div role="group" class="d-flex">
            <input
              ref="input"
              :id="inputid"
              v-model="newTag"
              type="text"
              :list="id"
              :placeholder="placeholder"
              autocomplete="off"
              class="w-100 flex-grow-1 p-0 m-0 bg-transparent border-0"
              @keydown="onInputKeydown"
              :style="{ outline: 0, minWidth: '5rem' }"
            />
          </div>
        </li>
      </ul>
      <div v-if="tagsOptions" class="mt-1">
        <CFormSelect @change="onChange" :disabled="disabled || availableOptions.length == 0" value="">
         <option :disabled="!availableOptions" value="" :selected="newTag == ''">{{ placeholder || 'Please select an option' }}</option>
          <option v-for="option in availableOptions" :key="option[primary] || option" :value="option[primary] || option">
            {{ option.name || option }}
          </option>
        </CFormSelect>
      </div>
    </div>
    <div v-if="required && !checkField" class="invalid-feedback">
      Field is required
    </div>
  </div>
</template>
<script>

import { ref, watch, nextTick, onMounted, computed } from "vue";
import { CODE_SPACE, CODE_BACKSPACE, CODE_DELETE, CODE_ENTER } from '@/utils/key-codes'
import { stopEvent } from '@/utils/api'
import { get_color, gen_color } from '@/utils/colors'

export default {
  name: 'SFormTags',
  emits: ['update:modelValue'],
  props: {
    modelValue: { type: Array, default: () => [] },
    placeholder: {type: String, default: () => ''},
    tagsOptions: { type: [Array, Boolean], default: false },
    lockedTags: { type: Array, default: () => [] },
    allowCustom: { type: Boolean, default: true },
    disabled: { type: Boolean, default: false },
    primary: { type: String },
    size: { type: String, default: () => '' },
    colorize: { type: Boolean, default: false },
    required: {type: Boolean, default: () => false},
    trim: {type: Boolean, default: () => false},
  },
  data () {
    return {
      gen_color: gen_color,
      get_color: get_color,
      tags: ref(this.modelValue),
      newTag: ref(""),
      id: Math.random().toString(36).substring(7),
      inputid: Math.random().toString(36).substring(7),
      duplicate: ref(null),
      paddingLeft: ref(10),
      hasFocus: false,
      tagsUl: this.$refs.tagsUl,
      inputHandlers: this.computedInputHandlers,
    }
  },
  methods: {
    // Tags
    addTag (tag)  {
      tag = typeof tag === 'string' ? tag : this.newTag
      if (!tag || tag.toString().trim() === '') return // prevent empty tag
      // only allow predefined tags when allowCustom is false
      if (!this.allowCustom && !this.tagsOptions.includes(tag)) return
      // return early if duplicate
      if (this.trim) {
        tag = tag.toString().trim()
      }
      if (this.tags.includes(tag)) {
        this.handleDuplicate(tag)
        return
      }
      this.tags.push(tag)
      this.newTag = "" // reset newTag
    },
    removeTag (index) {
      this.tags.splice(index, 1)
      this.$nextTick(() => {
        this.focus()
      })
    },
    handleDuplicate (tag) {
      this.duplicate = tag
      setTimeout(() => (this.duplicate = null), 1000)
      this.newTag = ""
    },
    focus() {
      if (!this.disabled && !this.tagsOptions) {
        this.$refs.input.focus()
      }
    },
    onInputKeydown (event) {
      if (!(event instanceof Event)) {
        return
      }
      const { keyCode } = event
      const value = event.target.value || ''
      if (keyCode === CODE_ENTER || keyCode === CODE_SPACE) {
        stopEvent(event, { propagation: false })
        this.addTag()
      } else if ((keyCode === CODE_BACKSPACE || keyCode === CODE_DELETE) && value === '') {
        stopEvent(event, { propagation: false })
        this.removeTag(this.tags.length - 1)
      }
    },
    onFocusin() {
      this.hasFocus = true
    },
    onFocusout() {
      this.hasFocus = false
      this.addTag()
    },
    onChange (event) {
      var ntag = this.availableOptions[event.target.selectedIndex-1]
      this.addTag(ntag.name || ntag)
    }
  },
  computed: {
    // options
    availableOptions () {
      if (!this.tagsOptions) return false;
      return this.tagsOptions.filter((option) => !this.tags.includes(option[this.primary] || option));
    },
    checkField () {
      return this.tags.length > 0
    },
  },
  watch: {
    tags: {
      handler: function() {
      	this.$emit("update:modelValue", this.tags)
      },
      deep: true,
    }
  }
};
</script>
<style scoped>
.c-form-tags, .c-form-tags-list {
  margin-top: -.25rem;
}
.c-form-tags-field {
  margin-top: .25rem;
}
.c-form-tag {
  margin-top: .25rem;
  margin-right: .25rem;
}
.focus {
  color: #495057;
  background-color: white;
  border-color: #80bdff;
  outline: 0;
  -webkit-box-shadow: 0 0 0 0.2rem rgb(0 123 255 / 25%);
  box-shadow: 0 0 0 0.2rem rgb(0 123 255 / 25%);
}
.delete {
  color: white;
  background: none;
  outline: none;
  border: none;
  cursor: pointer;
}
@keyframes shake {
  10%,
  90% {
    transform: scale(0.9) translate3d(-1px, 0, 0);
  }

  20%,
  80% {
    transform: scale(0.9) translate3d(2px, 0, 0);
  }

  30%,
  50%,
  70% {
    transform: scale(0.9) translate3d(-4px, 0, 0);
  }

  40%,
  60% {
    transform: scale(0.9) translate3d(4px, 0, 0);
  }
}
.duplicate {
  background: rgb(235, 27, 27);
  animation: shake 1s;
}
</style>
