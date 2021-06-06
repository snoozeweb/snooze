export default [
  {
    _name: 'CSidebarNav',
    _children: [
      {
        _name: 'CSidebarNavItem',
        name: 'Dashboard',
        to: '/dashboard',
        fontIcon: 'la la-tachometer',
      },
      {
        _name: 'CSidebarNavItem',
        name: 'Alerts',
        to: '/record',
        fontIcon: 'la la-file-text'
      },
      {
        _name: 'CSidebarNavItem',
        name: 'Aggregates',
        to: '/aggregate',
        fontIcon: 'la la-folder',
      },
      {
        _name: 'CSidebarNavTitle',
        _children: ['Core']
      },
      {
        _name: 'CSidebarNavItem',
        name: 'Rules',
        to: '/rule',
        fontIcon: 'la la-balance-scale'
      },
      {
        _name: 'CSidebarNavItem',
        name: 'Aggregate Rules',
        to: '/aggregaterule',
        fontIcon: 'la la-suitcase'
      },
      {
        _name: 'CSidebarNavItem',
        name: 'Snooze',
        to: '/snooze',
        fontIcon: 'la la-bell-slash'
      },
      {
        _name: 'CSidebarNavItem',
        name: 'Notifications',
        to: '/notification',
        fontIcon: 'la la-bullhorn'
      },
      {
        _name: 'CSidebarNavTitle',
        _children: ['Utils']
      },
      {
        _name: 'CSidebarNavItem',
        name: 'Commands',
        to: '/command',
        fontIcon: 'la la-code',
      },
      {
        _name: 'CSidebarNavTitle',
        _children: ['Customize']
      },
      {
        _name: 'CSidebarNavItem',
        name: 'Users',
        to: '/user',
        fontIcon: 'la la-users',
      },
      {
        _name: 'CSidebarNavItem',
        name: 'Roles',
        to: '/role',
        fontIcon: 'la la-user-friends',
      },
      {
        _name: 'CSidebarNavItem',
        name: 'Settings',
        to: '/settings',
        fontIcon: 'la la-cog',
      },
      {
        _name: 'CSidebarNavItem',
        name: 'Profile',
        to: '/profile',
        fontIcon: 'la la-sliders',
      },
    ]
  }
]
