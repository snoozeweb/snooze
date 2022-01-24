import { createRouter, createWebHashHistory } from 'vue-router'
import DefaultLayout from '@/containers/DefaultLayout'

// Components
const Record = () => import('@/views/Record')
const Dashboard = () => import('@/views/Dashboard')
const Status = () => import('@/views/Status')
const Rule = () => import('@/views/Rule')
const AggregateRule = () => import('@/views/AggregateRule')
const Snooze = () => import('@/views/Snooze')
const Notification = () => import('@/views/Notification')
const Action = () => import('@/views/Action')
const Widget = () => import('@/views/Widget')
const KeyValue = () => import('@/views/KeyValue')
const User = () => import('@/views/User')
const Role = () => import('@/views/Role')
const Environment = () => import('@/views/Environment')
const Settings = () => import('@/views/Settings')
const Profile = () => import('@/views/Profile')
const Login = () => import('@/views/Login')

const router = createRouter({
  history: createWebHashHistory(process.env.BASE_URL),
  scrollBehavior: () => ({ top: 0 }),
  routes: [
    {
      path: '/login',
      name: 'Login',
      component: Login
    },
    {
      path: '/',
      redirect: '/record',
      name: 'Home',
      component: DefaultLayout,
      children: [
        {
          path: 'record',
          name: 'Records',
          component: Record
        },
        {
          path: 'dashboard',
          name: 'Dashboard',
          component: Dashboard
        },
        {
          path: 'status',
          name: 'Status',
          component: Status
        },
        {
          path: 'rule',
          name: 'Rules',
          component: Rule
        },
        {
          path: 'aggregaterule',
          name: 'Aggregate Rules',
          component: AggregateRule
        },
        {
          path: 'snooze',
          meta: {label: 'Snooze'},
          component: Snooze,
        },
        {
          path: 'snooze/*',
          meta: {label: 'Snooze'},
          component: Snooze,
        },
        {
          path: '/notification',
          name: 'Notifications',
          component: Notification,
        },
        {
          path: '/action',
          name: 'Actions',
          component: Action,
        },
        {
          path: '/widget',
          name: 'Widgets',
          component: Widget,
        },
        {
          path: '/kv',
          name: 'Key-values',
          component: KeyValue,
        },
        {
          path: '/user',
          name: 'Users',
          component: User,
        },
        {
          path: '/role',
          name: 'Roles',
          component: Role,
        },
        {
          path: '/Environment',
          name: 'Environments',
          component: Environment,
        },
        {
          path: '/settings',
          name: 'Settings',
          component: Settings,
        },
        {
          path: '/profile',
          name: 'Profile',
          component: Profile,
        },
      ]
    },
  ]
})

export default router
